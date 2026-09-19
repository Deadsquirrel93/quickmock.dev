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

	if subcommand != "create" {
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", subcommand)
		printUsageAndExit()
	}

	createCmd := flag.NewFlagSet("create", flag.ExitOnError)
	method := createCmd.String("m", "GET", "HTTP method (GET, POST, PUT, etc.)")
	status := createCmd.Int("s", 200, "HTTP response status code")
	body := createCmd.String("b", "", "HTTP response body")
	baseURL := createCmd.String("url", "https://quickmock.dev", "Base URL of the Quickmock instance")
	contentType := createCmd.String("c", "application/json", "Content-Type for the response")

	// Optional multiple headers can be tricky with standard flag package, but we can do a simple comma-separated or multiple string slices if we define a custom flag,
	// let's stick to a simpler approach or a custom string slice flag.
	var headers stringSlice
	createCmd.Var(&headers, "h", "Additional response header (can be specified multiple times, e.g., -h 'X-Custom: 1')")

	createCmd.Parse(os.Args[2:])

	headerMap := make(map[string]string)
	for _, h := range headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			headerMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	reqBody := map[string]interface{}{
		"method":           *method,
		"response_status":  *status,
		"response_body":    *body,
		"content_type":     *contentType,
		"response_headers": headerMap,
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

func printUsageAndExit() {
	fmt.Fprintf(os.Stderr, "Usage: quickmock <command> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  create    Create a new mock endpoint\n\n")
	fmt.Fprintf(os.Stderr, "Example:\n")
	fmt.Fprintf(os.Stderr, "  quickmock create -m POST -b '{\"ok\":true}'\n")
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
