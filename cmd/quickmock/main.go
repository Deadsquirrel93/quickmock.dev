// Command quickmock creates Quickmock mocks from a shell, so a CI job or a
// Makefile can get a throwaway HTTP endpoint without opening a browser.
//
// It is a thin client over POST /api/mocks: every flag maps to a field of
// that request, and the two lines it prints are the mock's public URL and
// its one-time admin token.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		printUsageAndExit()
	}

	subcommand := os.Args[1]

	switch subcommand {
	case "create":
	case "-h", "-help", "--help", "help":
		printUsageAndExit()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", subcommand)
		printUsageAndExit()
	}

	createCmd := flag.NewFlagSet("create", flag.ExitOnError)
	method := createCmd.String("m", "GET", "HTTP method (GET, POST, PUT, etc.)")
	status := createCmd.Int("s", 200, "HTTP response status code")
	body := createCmd.String("b", "", "HTTP response body")
	baseURL := createCmd.String("url", "https://quickmock.dev", "Base URL of the Quickmock instance")
	contentType := createCmd.String("c", "application/json", "Content-Type for the response")
	ttl := createCmd.Int("ttl", 0, "Lifetime in seconds (0 keeps the server default)")

	// Repeatable, like curl's own -H. The name is capital H on purpose: the
	// flag package reserves -h for its usage output, and shadowing it turns
	// `quickmock create -h` into an "flag needs an argument" error instead of
	// the help text every user expects there.
	var headers stringSlice
	createCmd.Var(&headers, "H", "Response header, repeatable (e.g. -H 'X-Custom: 1')")

	createCmd.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: quickmock create [options]\n\nOptions:\n")
		createCmd.PrintDefaults()
	}

	_ = createCmd.Parse(os.Args[2:])

	headerMap, err := parseHeaders(headers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	reqBody := map[string]any{
		"method":           *method,
		"response_status":  *status,
		"response_body":    *body,
		"content_type":     *contentType,
		"response_headers": headerMap,
	}
	if *ttl > 0 {
		reqBody["ttl_seconds"] = *ttl
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error encoding payload: %v\n", err)
		os.Exit(1)
	}

	target := strings.TrimRight(*baseURL, "/") + "/api/mocks"

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "quickmock-cli/1.0")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error making request: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading response: %v\n", err)
		os.Exit(1)
	}

	if resp.StatusCode != http.StatusCreated {
		fmt.Fprintf(os.Stderr, "server returned %s\n%s\n", resp.Status, string(respBytes))
		os.Exit(1)
	}

	var result struct {
		URL        string `json:"url"`
		AdminToken string `json:"admin_token"`
	}

	if err := json.Unmarshal(respBytes, &result); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Public URL:  %s\n", result.URL)
	fmt.Printf("Admin Token: %s\n", result.AdminToken)
}

// parseHeaders turns repeated -H values into the response_headers map.
//
// A value without a colon is an error rather than a skip: silently dropping
// it would print a URL and a token as if everything worked, and the missing
// header would only surface later, in whatever test consumes the mock.
func parseHeaders(raw []string) (map[string]string, error) {
	out := make(map[string]string, len(raw))
	for _, h := range raw {
		name, value, ok := strings.Cut(h, ":")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid header %q: want \"Name: value\"", h)
		}
		out[name] = value
	}
	return out, nil
}

func printUsageAndExit() {
	fmt.Fprintf(os.Stderr, "Usage: quickmock <command> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  create    Create a new mock endpoint\n\n")
	fmt.Fprintf(os.Stderr, "Run 'quickmock create -help' for the full option list.\n\n")
	fmt.Fprintf(os.Stderr, "Example:\n")
	fmt.Fprintf(os.Stderr, "  quickmock create -m POST -s 201 -b '{\"ok\":true}'\n")
	os.Exit(1)
}

type stringSlice []string

func (s *stringSlice) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}
