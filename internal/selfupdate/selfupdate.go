package selfupdate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

const repo = "anish749/anthropic-proxy"
const checkInterval = 24 * time.Hour

// Update checks for the latest release and replaces the binary if a newer version exists.
// This is the explicit "anthropic-proxy update" flow.
func Update(currentVersion string) error {
	token, err := resolveGitHubToken()
	if err != nil {
		return err
	}
	return doUpdate(currentVersion, token, true)
}

// AutoCheck checks for updates if 24 hours have passed since the last check.
// Runs silently — errors are swallowed so it never blocks the user's command.
// Messages go to stderr so stdout stays clean for output.
func AutoCheck(currentVersion string) {
	if currentVersion == "dev" {
		return
	}

	lastCheck, _ := readLastCheck()
	if time.Since(lastCheck) < checkInterval {
		return
	}

	// Write timestamp now so we don't re-check on every command if token is missing.
	writeLastCheck()

	token, err := resolveGitHubToken()
	if err != nil {
		return // silently skip — no gh or token available
	}

	if err := doUpdate(currentVersion, token, false); err != nil {
		// Silent — don't disrupt the user's command.
		return
	}
}

func doUpdate(currentVersion string, token string, verbose bool) error {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{
		APIToken: token,
	})
	if err != nil {
		return fmt.Errorf("create update source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:    source,
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
	})
	if err != nil {
		return fmt.Errorf("create updater: %w", err)
	}

	latest, found, err := updater.DetectLatest(context.Background(), selfupdate.ParseSlug(repo))
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}
	if !found {
		if verbose {
			return fmt.Errorf("no releases found for %s", repo)
		}
		return nil
	}

	if latest.LessOrEqual(currentVersion) {
		if verbose {
			fmt.Fprintf(os.Stderr, "Already up to date (v%s)\n", currentVersion)
		}
		return nil
	}

	fmt.Fprintf(os.Stderr, "Updating anthropic-proxy v%s → v%s...\n", currentVersion, latest.Version())

	exePath, err := selfupdate.ExecutablePath()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}

	if err := updater.UpdateTo(context.Background(), latest, exePath); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Updated to v%s\n", latest.Version())
	return nil
}

func lastCheckPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config", "anthropic-proxy", "last_update_check")
}

func readLastCheck() (time.Time, error) {
	data, err := os.ReadFile(lastCheckPath())
	if err != nil {
		return time.Time{}, err
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(ts, 0), nil
}

func writeLastCheck() {
	path := lastCheckPath()
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0600)
}

// resolveGitHubToken gets a GitHub token from env or gh CLI.
func resolveGitHubToken() (string, error) {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	}

	if !isGhInstalled() {
		return "", fmt.Errorf(ghNotInstalledMsg)
	}

	token, err := ghAuthToken()
	if err != nil {
		return "", fmt.Errorf(ghNotAuthedMsg)
	}
	return token, nil
}

func isGhInstalled() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

func ghAuthToken() (string, error) {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("empty token")
	}
	return token, nil
}

const ghNotInstalledMsg = `GitHub CLI (gh) is not installed. It's needed to download updates from the private repo.

Install it:
  macOS:   brew install gh
  Linux:   https://github.com/cli/cli/blob/trunk/docs/install_linux.md

Then authenticate:
  $ gh auth login

Alternatively, set GITHUB_TOKEN in your environment.`

const ghNotAuthedMsg = `GitHub CLI is installed but not authenticated.

Run:
  $ gh auth login

Follow the prompts to authenticate with your GitHub account.
Then retry:
  $ anthropic-proxy update`
