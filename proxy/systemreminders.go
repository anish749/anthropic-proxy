package proxy

import (
	"encoding/json"
	"log/slog"
	"regexp"
)

var _ Extractor = (*SystemRemindersExtractor)(nil)

var systemReminderRe = regexp.MustCompile(`(?s)<system-reminder>\s*(.*?)\s*</system-reminder>`)

// SystemRemindersExtractor extracts <system-reminder> blocks from user messages.
type SystemRemindersExtractor struct{}

func (SystemRemindersExtractor) Name() string { return "system-reminders" }

func (SystemRemindersExtractor) Extract(body map[string]json.RawMessage) (json.RawMessage, bool) {
	raw, ok := body["messages"]
	if !ok {
		return nil, false
	}

	var msgs []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &msgs); err != nil {
		slog.Error("system-reminders: failed to parse messages", "err", err)
		return nil, false
	}

	var reminders []string
	for _, msg := range msgs {
		if msg.Role != "user" {
			continue
		}

		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			slog.Warn("system-reminders: failed to parse message content", "err", err)
			continue
		}

		for _, block := range blocks {
			if block.Type != "text" {
				continue
			}
			matches := systemReminderRe.FindAllStringSubmatch(block.Text, -1)
			for _, match := range matches {
				reminders = append(reminders, match[1])
			}
		}
	}

	if len(reminders) == 0 {
		return nil, false
	}

	out, err := json.Marshal(reminders)
	if err != nil {
		slog.Error("system-reminders: failed to marshal output", "err", err)
		return nil, false
	}
	return out, true
}
