package proxy

import (
	"bytes"
	"io"
	"log"
	"net/http"
)

const targetBase = "https://api.anthropic.com"

type Proxy struct {
	client     *http.Client
	fileLogger *FileLogger
	rewriter   *Rewriter
}

func New(logRequests bool) *Proxy {
	p := &Proxy{
		client:   &http.Client{},
		rewriter: NewRewriter("prompts"),
	}
	if logRequests {
		p.fileLogger = NewFileLogger("requests",
			[]Extractor{ToolsExtractor{}, MessagesExtractor{}, SystemExtractor{}},
			[]Extractor{UsageExtractor{}},
		)
	}
	return p
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

	// Rewrite system prompt if rules are configured
	rewrittenBody := p.rewriter.Rewrite(reqBody)

	// Build outgoing request
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(rewrittenBody))
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

	// Log request parts to files for Anthropic Messages API calls
	if p.fileLogger != nil && r.Method == http.MethodPost && r.URL.Path == "/v1/messages" {
		if reqID := resp.Header.Get("Request-Id"); reqID != "" {
			p.fileLogger.Log(reqID, reqBody, respBody)
		}
	}

	// Copy response headers back to client
	for key, vals := range resp.Header {
		for _, val := range vals {
			w.Header().Add(key, val)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}
