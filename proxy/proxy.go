package proxy

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
)

const targetBase = "https://api.anthropic.com"

// RequestLogLevel controls when requests are logged to disk.
type RequestLogLevel int

const (
	// LogOff never logs requests to disk.
	LogOff RequestLogLevel = iota
	// LogOnWarnings logs requests only when rewrite rules produce warnings.
	LogOnWarnings
	// LogAll logs every request.
	LogAll
)

// ParseRequestLogLevel parses a level name: "off", "warnings", or "all".
func ParseRequestLogLevel(s string) (RequestLogLevel, error) {
	switch s {
	case "off":
		return LogOff, nil
	case "warnings":
		return LogOnWarnings, nil
	case "all":
		return LogAll, nil
	}
	return LogOff, fmt.Errorf("invalid log level %q (want off, warnings, or all)", s)
}

func (l RequestLogLevel) String() string {
	switch l {
	case LogOnWarnings:
		return "warnings"
	case LogAll:
		return "all"
	default:
		return "off"
	}
}

type Proxy struct {
	client     *http.Client
	fileLogger *FileLogger
	logLevel   RequestLogLevel
	rewriter   *Rewriter
	credSwap   *CredSwapper
}

type Options struct {
	LogLevel  RequestLogLevel
	SwapCreds bool
	LogFormat string // "json" or "yaml"
}

func New(opts Options) *Proxy {
	p := &Proxy{
		client:   &http.Client{},
		rewriter: NewRewriter("prompts"),
		fileLogger: NewFileLogger("requests", opts.LogFormat,
			[]Extractor{ToolsExtractor{}, MessagesExtractor{}, SystemExtractor{}, SystemRemindersExtractor{}},
			[]Extractor{UsageExtractor{}},
		),
		logLevel: opts.LogLevel,
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

// WatchPrompts starts hot-reloading of prompt files in the background.
func (p *Proxy) WatchPrompts() {
	p.rewriter.Watch()
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

	// Log request parts to files for Anthropic Messages API calls,
	// depending on the configured level.
	if p.shouldLog(r, warnings) {
		if reqID != "" {
			p.fileLogger.Log(reqID, r, reqBody, rewrittenBody, respBody)
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

// shouldLog reports whether a request should be written to disk.
func (p *Proxy) shouldLog(r *http.Request, warnings *Warnings) bool {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
		return false
	}
	switch p.logLevel {
	case LogAll:
		return true
	case LogOnWarnings:
		return warnings.HasWarnings()
	default:
		return false
	}
}
