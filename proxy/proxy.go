package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"os"
)

const targetBase = "https://api.anthropic.com"

// RequestLogLevel controls when requests are logged to disk.
type RequestLogLevel int

const (
	// LogOnWarnings logs requests only when rewrite rules produce warnings.
	LogOnWarnings RequestLogLevel = iota
	// LogAll logs every request.
	LogAll
)

type Proxy struct {
	client   *http.Client
	fileLogger *FileLogger
	logLevel   RequestLogLevel
	rewriter   *Rewriter
	credSwap   *CredSwapper
}

type Options struct {
	LogRequests bool
	SwapCreds   bool
}

func New(opts Options) *Proxy {
	logLevel := LogOnWarnings
	if opts.LogRequests {
		logLevel = LogAll
	}
	p := &Proxy{
		client:   &http.Client{},
		rewriter: NewRewriter("prompts"),
		fileLogger: NewFileLogger("requests",
			[]Extractor{ToolsExtractor{}, MessagesExtractor{}, SystemExtractor{}},
			[]Extractor{UsageExtractor{}},
		),
		logLevel: logLevel,
	}
	if opts.SwapCreds {
		cs, err := NewCredSwapper()
		if err != nil {
			slog.Error("credswap: "+err.Error())
			os.Exit(1)
		}
		p.credSwap = cs
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
	rewrittenBody, warnings := p.rewriter.Rewrite(reqBody)

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

	// Swap credentials if enabled
	if p.credSwap != nil {
		if err := p.credSwap.SwapHeaders(outReq); err != nil {
			slog.Error("credswap: failed to swap credentials", "err", err)
			http.Error(w, "credential swap failed", http.StatusInternalServerError)
			return
		}
	}

	// Send request
	resp, err := p.client.Do(outReq)
	if err != nil {
		slog.Error("upstream error", "err", err)
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

	// Flush rewrite warnings with the upstream request ID for correlation
	reqID := resp.Header.Get("Request-Id")
	warnings.Flush(reqID)

	// Log request parts to files for Anthropic Messages API calls:
	// always when LogAll, or on-demand when there are rewrite warnings.
	shouldLog := r.Method == http.MethodPost && r.URL.Path == "/v1/messages" &&
		(p.logLevel >= LogAll || warnings.HasWarnings())
	if shouldLog {
		if reqID != "" {
			p.fileLogger.Log(reqID, reqBody, respBody)
		} else {
			slog.Error("filelogger: missing Request-Id header, skipping log")
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
