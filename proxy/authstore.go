package proxy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "anthropic-proxy"
	keyringUser    = "oauth"
)

// AuthStore abstracts credential persistence.
type AuthStore interface {
	Load() (*AuthData, error)
	Save(auth *AuthData) error
	Name() string
}

// NewAuthStore returns a keychain-backed store on macOS,
// falling back to file-based storage on other platforms.
func NewAuthStore() AuthStore {
	if runtime.GOOS == "darwin" {
		store := &keychainStore{}
		// Verify keychain access works by doing a no-op check.
		// If it fails (e.g., headless environment), fall back to file.
		if _, err := keyring.Get(keyringService, keyringUser); err != nil && err != keyring.ErrNotFound {
			slog.Warn("authstore: keychain unavailable, using file storage", "err", err)
			return &fileStore{}
		}
		return store
	}
	return &fileStore{}
}

// --- Keychain backend (macOS) ---

type keychainStore struct{}

func (k *keychainStore) Name() string { return "system keychain" }

func (k *keychainStore) Load() (*AuthData, error) {
	data, err := keyring.Get(keyringService, keyringUser)
	if err != nil {
		return nil, fmt.Errorf("keychain: %w", err)
	}
	var auth AuthData
	if err := json.Unmarshal([]byte(data), &auth); err != nil {
		return nil, fmt.Errorf("keychain: failed to parse stored data: %w", err)
	}
	return &auth, nil
}

func (k *keychainStore) Save(auth *AuthData) error {
	data, err := json.Marshal(auth)
	if err != nil {
		return err
	}
	return keyring.Set(keyringService, keyringUser, string(data))
}

// --- File backend (non-macOS fallback) ---

type fileStore struct{}

func (f *fileStore) Name() string { return authFilePath() }

func authDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".anthropic-proxy")
}

func authFilePath() string {
	return filepath.Join(authDir(), "auth.json")
}

func (f *fileStore) Load() (*AuthData, error) {
	data, err := os.ReadFile(authFilePath())
	if err != nil {
		return nil, err
	}
	var auth AuthData
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, err
	}
	return &auth, nil
}

func (f *fileStore) Save(auth *AuthData) error {
	if err := os.MkdirAll(authDir(), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(authFilePath(), data, 0600)
}
