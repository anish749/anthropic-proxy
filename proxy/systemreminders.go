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

		// Content can be a string or an array of blocks
		var texts []string
		var plainStr string
		if err := json.Unmarshal(msg.Content, &plainStr); err == nil {
			texts = append(texts, plainStr)
		} else {
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(msg.Content, &blocks); err != nil {
				userMsgIdx++
				continue
			}
			for _, block := range blocks {
				if block.Type == "text" {
					texts = append(texts, block.Text)
				}
			}
		}

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
