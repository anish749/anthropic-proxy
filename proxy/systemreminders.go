package proxy

import (
	"encoding/json"
	"log/slog"
	"regexp"
)

var _ Extractor = (*SystemRemindersExtractor)(nil)

var systemReminderRe = regexp.MustCompile(`(?s)<system-reminder>\s*(.*?)\s*</system-reminder>`)

type systemReminder struct {
	MessageIndex     int    `json:"message_index"`      // position in the full messages array (0-based)
	UserMessageIndex int    `json:"user_message_index"`  // position among user messages only (0-based)
	Turn             int    `json:"turn"`                // conversation turn (user+assistant = 1 turn, 1-based)
	Text             string `json:"text"`                // full text including <system-reminder> tags
}

// parseContentTexts extracts text strings from a message content field,
// handling both the string form ("content": "hello") and the content-block
// array form ("content": [{"type":"text","text":"hello"}]).
func parseContentTexts(raw json.RawMessage) []string {
	// Try plain string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []string{s}
	}
	// Otherwise treat as array of content blocks.
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		slog.Warn("system-reminders: failed to parse message content", "err", err)
		return nil
	}
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" {
			texts = append(texts, b.Text)
		}
	}
	return texts
}

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

	var reminders []systemReminder
	userMsgIdx := 0
	turn := 0
	for i, msg := range msgs {
		if msg.Role == "user" {
			turn++
		}
		if msg.Role != "user" {
			continue
		}

		texts := parseContentTexts(msg.Content)

		for _, text := range texts {
			matches := systemReminderRe.FindAllString(text, -1)
			for _, match := range matches {
				reminders = append(reminders, systemReminder{
					MessageIndex:     i,
					UserMessageIndex: userMsgIdx,
					Turn:             turn,
					Text:             match,
				})
			}
		}
		userMsgIdx++
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
