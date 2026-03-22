package systemreminders

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var systemReminderRe = regexp.MustCompile(`(?s)<system-reminder>\s*(.*?)\s*</system-reminder>`)

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Run scans the requests directory for messages files, extracts all unique
// <system-reminder> blocks from user text messages, and writes each to a
// separate file in the output directory.
func Run(requestsDir, outputDir string) error {
	entries, err := os.ReadDir(requestsDir)
	if err != nil {
		return fmt.Errorf("failed to read requests dir: %w", err)
	}

	type reminder struct {
		text       string
		firstSource string // earliest source filename (used for output naming)
		sources    []string
	}
	seen := make(map[string]*reminder)
	// Track per-source reminder count for numbering when a request has multiple reminders.
	sourceCount := make(map[string]int)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "-messages.json") {
			continue
		}

		path := filepath.Join(requestsDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("skipping unreadable file", "path", path, "err", err)
			continue
		}

		var msgs []message
		if err := json.Unmarshal(data, &msgs); err != nil {
			slog.Warn("skipping unparseable file", "path", path, "err", err)
			continue
		}

		for _, msg := range msgs {
			if msg.Role != "user" {
				continue
			}

			var blocks []contentBlock
			if err := json.Unmarshal(msg.Content, &blocks); err != nil {
				continue
			}

			for _, block := range blocks {
				if block.Type != "text" {
					continue
				}

				matches := systemReminderRe.FindAllStringSubmatch(block.Text, -1)
				for _, m := range matches {
					body := strings.TrimSpace(m[1])
					hash := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))[:12]

					if r, ok := seen[hash]; ok {
						r.sources = append(r.sources, entry.Name())
					} else {
						seen[hash] = &reminder{
							text:        body,
							firstSource: entry.Name(),
							sources:     []string{entry.Name()},
						}
					}
				}
			}
		}
	}

	if len(seen) == 0 {
		fmt.Println("No system reminders found.")
		return nil
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	for _, r := range seen {
		sourceCount[r.firstSource]++
		filename := fmt.Sprintf("%s-reminder-%d.txt", r.firstSource, sourceCount[r.firstSource])
		path := filepath.Join(outputDir, filename)

		var buf strings.Builder
		fmt.Fprintf(&buf, "matched_requests: %d\n", len(r.sources))
		buf.WriteString("request_logs:\n")
		for _, src := range r.sources {
			fmt.Fprintf(&buf, "  - %s\n", src)
		}
		buf.WriteString("---\n\n")
		buf.WriteString("<system-reminder>\n")
		buf.WriteString(r.text)
		buf.WriteString("\n</system-reminder>\n")

		if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
	}

	fmt.Printf("Extracted %d unique system reminders to %s/\n", len(seen), outputDir)
	return nil
}
