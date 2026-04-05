package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const updateCheckInterval = 24 * time.Hour

// checkForUpdateSilently runs a non-blocking update check. It must never
// panic, block, or print errors — all failures are silently ignored.
func checkForUpdateSilently() {
	defer func() { _ = recover() }()

	if version == "dev" {
		return
	}

	stateDir := configDir()
	if stateDir == "" {
		return
	}

	tsFile := filepath.Join(stateDir, "last_update_check")

	if !shouldCheck(tsFile) {
		return
	}

	rel, err := fetchLatestRelease()
	if err != nil {
		return
	}

	// Update the timestamp regardless of whether a new version exists.
	_ = writeTimestamp(tsFile)

	latestVersion := strings.TrimPrefix(rel.TagName, "v")
	currentVersion := strings.TrimPrefix(version, "v")

	if latestVersion != currentVersion && latestVersion != "" {
		fmt.Fprintf(os.Stderr, "A new version of anthropic-proxy is available: v%s (current: v%s). Run 'anthropic-proxy update' to upgrade.\n", latestVersion, currentVersion)
	}
}

// configDir returns the XDG-compatible config directory for anthropic-proxy.
func configDir() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "anthropic-proxy")
}

// shouldCheck returns true if we should perform an update check.
func shouldCheck(tsFile string) bool {
	data, err := os.ReadFile(tsFile)
	if err != nil {
		// File doesn't exist or can't be read — do the check.
		return true
	}

	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return true
	}

	return time.Since(t) >= updateCheckInterval
}

// writeTimestamp writes the current time to the timestamp file.
func writeTimestamp(tsFile string) error {
	dir := filepath.Dir(tsFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(tsFile, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644)
}
