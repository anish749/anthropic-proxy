package proxy

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// oauthHeaders are the headers required when using an OAuth token,
// matching the behavior of Claude Code / pi-mono.
var oauthHeaders = map[string]string{
	"Anthropic-Beta":                              "claude-code-20250219,oauth-2025-04-20,fine-grained-tool-streaming-2025-05-14,interleaved-thinking-2025-05-14",
	"User-Agent":                                  "claude-cli/2.1.62 (external, cli)",
	"X-App":                                       "cli",
	"Anthropic-Dangerous-Direct-Browser-Access":   "true",
}

func maskValue(v string) string {
	if len(v) <= 12 {
		return "***"
	}
	return v[:6] + "..." + v[len(v)-4:]
}

// CredSwapper loads OAuth credentials from auth.json and swaps
// the Authorization header on outgoing proxy requests.
// It auto-refreshes expired tokens.
type CredSwapper struct {
	mu   sync.Mutex
	auth *AuthData
}

// NewCredSwapper loads credentials from ~/.anthropic-proxy/auth.json.
// Returns an error if credentials are not found (user should run "login" first).
func NewCredSwapper() (*CredSwapper, error) {
	auth, err := loadAuth()
	if err != nil {
		return nil, fmt.Errorf("no credentials found — run 'login' first: %w", err)
	}
	log.Printf("[credswap] loaded OAuth token: %s", maskValue(auth.Access))
	return &CredSwapper{auth: auth}, nil
}

// ensureFresh checks if the token is expired and refreshes if needed.
func (cs *CredSwapper) ensureFresh() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if time.Now().UnixMilli() < cs.auth.Expires {
		return nil
	}

	log.Printf("[credswap] token expired, refreshing...")
	tok, err := refreshToken(cs.auth.Refresh)
	if err != nil {
		return fmt.Errorf("token refresh failed: %w", err)
	}

	cs.auth.Access = tok.AccessToken
	cs.auth.Refresh = tok.RefreshToken
	cs.auth.Expires = time.Now().UnixMilli() + (tok.ExpiresIn * 1000) - expiryBuffer.Milliseconds()

	if err := saveAuth(cs.auth); err != nil {
		log.Printf("[credswap] warning: failed to persist refreshed token: %v", err)
	}

	log.Printf("[credswap] token refreshed successfully")
	return nil
}

// SwapHeaders replaces credential headers on the outgoing request with
// the OAuth token and sets the required OAuth headers.
func (cs *CredSwapper) SwapHeaders(req *http.Request) error {
	if err := cs.ensureFresh(); err != nil {
		return err
	}

	cs.mu.Lock()
	token := cs.auth.Access
	cs.mu.Unlock()

	// Remove any existing credential headers from the client
	req.Header.Del("Authorization")
	req.Header.Del("X-Api-Key")
	req.Header.Del("Anthropic-Api-Key")

	// Set OAuth bearer token
	req.Header.Set("Authorization", "Bearer "+token)

	// Set required OAuth headers, preserving any existing anthropic-beta values
	for key, val := range oauthHeaders {
		if strings.EqualFold(key, "Anthropic-Beta") {
			existing := req.Header.Get("Anthropic-Beta")
			if existing != "" {
				// Merge: add our betas if not already present
				req.Header.Set("Anthropic-Beta", mergeBetas(existing, val))
			} else {
				req.Header.Set("Anthropic-Beta", val)
			}
		} else {
			req.Header.Set(key, val)
		}
	}

	return nil
}

// mergeBetas combines two comma-separated beta lists, deduplicating.
func mergeBetas(existing, extra string) string {
	seen := make(map[string]bool)
	var result []string
	for _, b := range strings.Split(existing, ",") {
		b = strings.TrimSpace(b)
		if b != "" && !seen[b] {
			seen[b] = true
			result = append(result, b)
		}
	}
	for _, b := range strings.Split(extra, ",") {
		b = strings.TrimSpace(b)
		if b != "" && !seen[b] {
			seen[b] = true
			result = append(result, b)
		}
	}
	return strings.Join(result, ",")
}
