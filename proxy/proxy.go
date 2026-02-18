package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

const targetBase = "https://api.anthropic.com"

type Proxy struct {
	client *http.Client
}

func New() *Proxy {
	return &Proxy{
		client: &http.Client{},
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	targetURL := targetBase + r.URL.Path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Read request body
	var reqBody []byte
	if r.Body != nil {
		var err error
		reqBody, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusInternalServerError)
			return
		}
		r.Body.Close()
	}

	// Log request
	logRequest(r, targetURL, reqBody)

	// Build outgoing request
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}

	// Copy all headers as-is
	for key, vals := range r.Header {
		for _, val := range vals {
			outReq.Header.Add(key, val)
		}
	}

	// Send request
	resp, err := p.client.Do(outReq)
	if err != nil {
		log.Printf("upstream error: %v", err)
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "failed to read response body", http.StatusBadGateway)
		return
	}

	// Log response
	logResponse(resp, respBody)

	// Copy response headers back to client
	for key, vals := range resp.Header {
		for _, val := range vals {
			w.Header().Add(key, val)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

func logRequest(r *http.Request, targetURL string, body []byte) {
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("► REQUEST  %s %s\n", r.Method, targetURL)
	fmt.Println(strings.Repeat("─", 60))

	fmt.Println("Headers:")
	for key, vals := range r.Header {
		for _, val := range vals {
			fmt.Printf("  %s: %s\n", key, val)
		}
	}

	if len(body) > 0 {
		fmt.Println("\nBody:")
		printJSON(body)
	}
	fmt.Println()
}

func logResponse(resp *http.Response, body []byte) {
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("◄ RESPONSE %d %s\n", resp.StatusCode, resp.Status)
	fmt.Println(strings.Repeat("─", 60))

	fmt.Println("Headers:")
	for key, vals := range resp.Header {
		for _, val := range vals {
			fmt.Printf("  %s: %s\n", key, val)
		}
	}

	if len(body) > 0 {
		fmt.Println("\nBody:")
		printJSON(body)
	}
	fmt.Println()
}

func printJSON(data []byte) {
	var buf bytes.Buffer
	if json.Indent(&buf, data, "  ", "  ") == nil {
		fmt.Printf("  %s\n", buf.String())
	} else {
		fmt.Printf("  %s\n", string(data))
	}
}
