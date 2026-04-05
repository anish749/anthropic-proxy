package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const (
	githubOwner = "anish749"
	githubRepo  = "anthropic-proxy"
	releaseAPI  = "https://api.github.com/repos/" + githubOwner + "/" + githubRepo + "/releases/latest"
)

// githubRelease is the subset of the GitHub releases API response we need.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update anthropic-proxy to the latest version",
	Long:  "Fetches the latest release from GitHub and replaces the current binary.",
	Args:  cobra.NoArgs,
	RunE:  runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	fmt.Printf("Current version: %s\n", version)
	fmt.Println("Checking for latest release...")

	rel, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to fetch latest release: %w", err)
	}

	latestVersion := strings.TrimPrefix(rel.TagName, "v")
	currentVersion := strings.TrimPrefix(version, "v")

	if latestVersion == currentVersion {
		fmt.Printf("Already up to date (%s).\n", version)
		return nil
	}

	fmt.Printf("New version available: %s\n", rel.TagName)

	// Determine the expected archive name.
	archiveName := fmt.Sprintf("anthropic-proxy_%s_%s_%s.tar.gz", latestVersion, runtime.GOOS, runtime.GOARCH)

	// Find the archive asset URL.
	var archiveURL string
	for _, a := range rel.Assets {
		if a.Name == archiveName {
			archiveURL = a.BrowserDownloadURL
			break
		}
	}
	if archiveURL == "" {
		return fmt.Errorf("no release asset found for %s/%s (expected %s)", runtime.GOOS, runtime.GOARCH, archiveName)
	}

	// Find the checksums asset URL.
	var checksumsURL string
	for _, a := range rel.Assets {
		if a.Name == "checksums.txt" {
			checksumsURL = a.BrowserDownloadURL
			break
		}
	}
	if checksumsURL == "" {
		return fmt.Errorf("checksums.txt not found in release assets")
	}

	// Download checksums.
	fmt.Println("Downloading checksums...")
	checksumsData, err := httpGetBytes(checksumsURL)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}

	expectedHash, err := findChecksum(checksumsData, archiveName)
	if err != nil {
		return err
	}

	// Download the archive.
	fmt.Printf("Downloading %s...\n", archiveName)
	archiveData, err := httpGetBytes(archiveURL)
	if err != nil {
		return fmt.Errorf("failed to download archive: %w", err)
	}

	// Validate the checksum.
	actualHash := sha256sum(archiveData)
	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	fmt.Println("Checksum verified.")

	// Extract the binary from the tarball.
	binaryData, err := extractFromTarGz(archiveData, "anthropic-proxy")
	if err != nil {
		return fmt.Errorf("failed to extract binary from archive: %w", err)
	}

	// Replace the current binary atomically.
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	if err := replaceBinary(execPath, binaryData); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	fmt.Printf("Successfully updated: %s -> %s\n", version, rel.TagName)
	return nil
}

// fetchLatestRelease queries the GitHub releases API.
func fetchLatestRelease() (*githubRelease, error) {
	req, err := http.NewRequest("GET", releaseAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// httpGetBytes fetches a URL and returns the response body as bytes.
func httpGetBytes(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s for %s", resp.Status, url)
	}

	return io.ReadAll(resp.Body)
}

// sha256sum returns the hex-encoded SHA-256 digest of data.
func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// findChecksum parses a goreleaser checksums.txt and returns the hash for the given filename.
func findChecksum(checksums []byte, filename string) (string, error) {
	for _, line := range strings.Split(strings.TrimSpace(string(checksums)), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == filename {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("checksum not found for %s", filename)
}

// replaceBinary atomically replaces the binary at path with newData.
func replaceBinary(path string, newData []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "anthropic-proxy-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(newData); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Preserve the original file's permissions.
	info, err := os.Stat(path)
	if err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, info.Mode()); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Atomic rename.
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
