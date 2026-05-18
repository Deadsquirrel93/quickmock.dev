package service

import (
	"fmt"
	"strings"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// CodeSnippet is one entry shown in the "Use this mock from your code" tabs.
type CodeSnippet struct {
	Lang  string // "js" / "python" / "go" / "php"
	Label string // display label
	Code  string
}

// MockURL returns the canonical public URL of a mock, including the user's
// optional path suffix. Use this everywhere a URL is shown to a user — it
// keeps the labelled path consistent across the detail page, share view,
// cURL snippet, and code samples.
func MockURL(m *model.Mock, baseURL string) string {
	u := strings.TrimRight(baseURL, "/") + "/m/" + m.Slug
	if m.PathSuffix != "" {
		u += "/" + m.PathSuffix
	}
	return u
}

// GenerateSnippets returns ready-to-paste client code for the four supported
// languages. The mock's URL is built from baseURL + slug; method defaults to
// GET when the mock allows ANY.
func GenerateSnippets(m *model.Mock, baseURL string) []CodeSnippet {
	url := MockURL(m, baseURL)
	method := string(m.Method)
	if method == "ANY" {
		method = "GET"
	}

	return []CodeSnippet{
		{Lang: "js", Label: "JavaScript", Code: jsSnippet(url, method, m)},
		{Lang: "python", Label: "Python", Code: pythonSnippet(url, method, m)},
		{Lang: "go", Label: "Go", Code: goSnippet(url, method, m)},
		{Lang: "php", Label: "PHP", Code: phpSnippet(url, method, m)},
	}
}

func jsSnippet(url, method string, m *model.Mock) string {
	var b strings.Builder
	fmt.Fprintf(&b, "const res = await fetch(%q, {\n", url)
	fmt.Fprintf(&b, "  method: %q,\n", method)
	if method != "GET" && method != "HEAD" && m.ResponseBody != "" {
		fmt.Fprintf(&b, "  headers: { 'Content-Type': %q },\n", m.ContentType)
		fmt.Fprintf(&b, "  body: %q,\n", m.ResponseBody)
	}
	b.WriteString("});\n")
	b.WriteString("const data = await res.text();\n")
	b.WriteString("console.log(res.status, data);")
	return b.String()
}

func pythonSnippet(url, method string, m *model.Mock) string {
	var b strings.Builder
	b.WriteString("import requests\n\n")
	switch method {
	case "GET", "HEAD":
		fmt.Fprintf(&b, "res = requests.%s(%q)\n", strings.ToLower(method), url)
	default:
		fmt.Fprintf(&b, "res = requests.%s(\n    %q,\n", strings.ToLower(method), url)
		if m.ResponseBody != "" {
			fmt.Fprintf(&b, "    headers={'Content-Type': %q},\n", m.ContentType)
			fmt.Fprintf(&b, "    data=%q,\n", m.ResponseBody)
		}
		b.WriteString(")\n")
	}
	b.WriteString("print(res.status_code, res.text)")
	return b.String()
}

func goSnippet(url, method string, m *model.Mock) string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"io\"\n")
	if method != "GET" && method != "HEAD" {
		b.WriteString("\t\"strings\"\n")
	}
	b.WriteString("\t\"net/http\"\n")
	b.WriteString(")\n\n")
	b.WriteString("func main() {\n")
	if method == "GET" {
		fmt.Fprintf(&b, "\tres, err := http.Get(%q)\n", url)
	} else {
		if m.ResponseBody != "" {
			fmt.Fprintf(&b, "\tbody := strings.NewReader(%q)\n", m.ResponseBody)
			fmt.Fprintf(&b, "\treq, err := http.NewRequest(%q, %q, body)\n", method, url)
			b.WriteString("\tif err != nil { panic(err) }\n")
			fmt.Fprintf(&b, "\treq.Header.Set(\"Content-Type\", %q)\n", m.ContentType)
			b.WriteString("\tres, err := http.DefaultClient.Do(req)\n")
		} else {
			fmt.Fprintf(&b, "\treq, _ := http.NewRequest(%q, %q, nil)\n", method, url)
			b.WriteString("\tres, err := http.DefaultClient.Do(req)\n")
		}
	}
	b.WriteString("\tif err != nil { panic(err) }\n")
	b.WriteString("\tdefer res.Body.Close()\n")
	b.WriteString("\tdata, _ := io.ReadAll(res.Body)\n")
	b.WriteString("\tfmt.Println(res.StatusCode, string(data))\n")
	b.WriteString("}")
	return b.String()
}

func phpSnippet(url, method string, m *model.Mock) string {
	var b strings.Builder
	b.WriteString("<?php\n")
	fmt.Fprintf(&b, "$ch = curl_init(%q);\n", url)
	fmt.Fprintf(&b, "curl_setopt($ch, CURLOPT_CUSTOMREQUEST, %q);\n", method)
	b.WriteString("curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);\n")
	if method != "GET" && method != "HEAD" && m.ResponseBody != "" {
		fmt.Fprintf(&b, "curl_setopt($ch, CURLOPT_HTTPHEADER, ['Content-Type: %s']);\n", m.ContentType)
		fmt.Fprintf(&b, "curl_setopt($ch, CURLOPT_POSTFIELDS, %q);\n", m.ResponseBody)
	}
	b.WriteString("$res = curl_exec($ch);\n")
	b.WriteString("$code = curl_getinfo($ch, CURLINFO_HTTP_CODE);\n")
	b.WriteString("curl_close($ch);\n")
	b.WriteString("echo $code, \"\\n\", $res;")
	return b.String()
}

// CurlSnippet returns a one-liner cURL command for the detail page's
// "Copy as cURL" button. Always uses GET when the mock allows ANY.
func CurlSnippet(m *model.Mock, baseURL string) string {
	url := MockURL(m, baseURL)
	method := string(m.Method)
	if method == "ANY" {
		method = "GET"
	}
	if method == "GET" {
		return fmt.Sprintf("curl -i %s", shellQuote(url))
	}
	parts := []string{"curl -i", "-X " + method, shellQuote(url)}
	if m.ResponseBody != "" {
		parts = append(parts, "-H 'Content-Type: "+m.ContentType+"'")
		parts = append(parts, "-d "+shellQuote(m.ResponseBody))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	// Single-quote for safety, escape any embedded single quotes.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
