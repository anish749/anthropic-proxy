package proxy

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	clientID    = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	authorizeURL = "https://claude.ai/oauth/authorize"
	tokenURL     = "https://console.anthropic.com/v1/oauth/token"
	redirectURI  = "https://console.anthropic.com/oauth/code/callback"
	scopes       = "org:create_api_key user:profile user:inference"

	expiryBuffer = 5 * time.Minute
)

// AuthData is the credential stored in auth.json.
type AuthData struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
	Expires int64  `json:"expires"` // unix millis
}

// generatePKCE creates a PKCE code_verifier and S256 code_challenge.
func generatePKCE() (verifier, challenge string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)

	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

// openBrowser attempts to open a URL in the default browser.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	default:
		return fmt.Errorf("unsupported platform")
	}
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
}

// exchangeCode exchanges an authorization code for tokens.
func exchangeCode(code, state, verifier string) (*tokenResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     clientID,
		"code":          code,
		"state":         state,
		"redirect_uri":  redirectURI,
		"code_verifier": verifier,
	})

	resp, err := http.Post(tokenURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, respBody)
	}

	var tok tokenResponse
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}
	return &tok, nil
}

// refreshToken uses a refresh token to get a new access token.
func refreshToken(refresh string) (*tokenResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     clientID,
		"refresh_token": refresh,
	})

	resp, err := http.Post(tokenURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed (%d): %s", resp.StatusCode, respBody)
	}

	var tok tokenResponse
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return nil, fmt.Errorf("failed to parse refresh response: %w", err)
	}
	return &tok, nil
}

// RunLogin performs the interactive OAuth login flow.
func RunLogin() error {
	store := NewAuthStore()

	verifier, challenge, err := generatePKCE()
	if err != nil {
		return fmt.Errorf("failed to generate PKCE: %w", err)
	}

	params := url.Values{
		"code":                  {"true"},
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirectURI},
		"scope":                 {scopes},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {verifier},
	}
	authURL := authorizeURL + "?" + params.Encode()

	fmt.Println("Opening browser for Anthropic OAuth login...")
	fmt.Println()
	fmt.Println("If the browser doesn't open, visit this URL manually:")
	fmt.Println(authURL)
	fmt.Println()

	_ = openBrowser(authURL)

	fmt.Print("Paste the authorization code (code#state): ")
	var input string
	fmt.Scanln(&input)

	input = strings.TrimSpace(input)
	parts := strings.SplitN(input, "#", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid code format, expected code#state")
	}
	code, state := parts[0], parts[1]

	fmt.Println("Exchanging code for tokens...")
	tok, err := exchangeCode(code, state, verifier)
	if err != nil {
		return err
	}

	expiresAt := time.Now().UnixMilli() + (tok.ExpiresIn * 1000) - expiryBuffer.Milliseconds()
	auth := &AuthData{
		Access:  tok.AccessToken,
		Refresh: tok.RefreshToken,
		Expires: expiresAt,
	}

	if err := store.Save(auth); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	fmt.Printf("Logged in successfully. Credentials saved to %s\n", store.Name())
	return nil
}
