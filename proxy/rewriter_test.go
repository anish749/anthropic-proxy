package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

const attributionReminder = "<system-reminder>\nAttribution for git commits and pull requests you create from here on (this replaces Claude Code's own earlier attribution guidance, such as a previous copy of this reminder; the user's own instructions about these lines, such as a CLAUDE.md or memory rule, take precedence over this reminder, but do not add attribution lines this reminder leaves out):\n- End git commit messages with:\nCo-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>\n- End pull request descriptions with:\n\U0001F916 Generated with [Claude Code](https://claude.com/claude-code)\n</system-reminder>\n"

// TestAttributionReminderRemoved loads the real prompts/ rules and checks that
// the attribution reminder, which arrives as its own text block, is dropped
// entirely instead of leaving behind an empty text block.
func TestAttributionReminderRemoved(t *testing.T) {
	state, err := loadRules("../prompts")
	if err != nil {
		t.Fatalf("loadRules: %v", err)
	}
	if len(state.reminderRules) == 0 {
		t.Fatal("expected at least one system-reminder rule in prompts/")
	}
	rw := &Rewriter{dir: "../prompts"}
	rw.state.Store(state)

	body := map[string]any{
		"model": "claude-fable-5-1",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "<system-reminder>\nCodebase instructions here.\n</system-reminder>"},
					map[string]any{"type": "text", "text": attributionReminder},
					map[string]any{"type": "text", "text": "hello"},
				},
			},
		},
	}
	raw, _ := json.Marshal(body)

	out, _ := rw.Rewrite(raw)

	var parsed struct {
		Messages []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal rewritten body: %v", err)
	}
	content := parsed.Messages[0].Content
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks after removal, got %d: %+v", len(content), content)
	}
	for _, b := range content {
		if strings.TrimSpace(b.Text) == "" {
			t.Fatalf("found empty text block: %+v", content)
		}
		if strings.Contains(b.Text, "Co-Authored-By") {
			t.Fatalf("attribution reminder still present: %q", b.Text)
		}
	}
	if content[0].Text != "<system-reminder>\nCodebase instructions here.\n</system-reminder>" {
		t.Errorf("unrelated reminder was altered: %q", content[0].Text)
	}
	if content[1].Text != "hello" {
		t.Errorf("user text was altered: %q", content[1].Text)
	}
}

// TestReminderRemovalKeepsLastBlock guards the edge case where the only block
// in a message is the removed reminder: the block is emptied but not dropped,
// so the message still has content.
func TestReminderRemovalKeepsLastBlock(t *testing.T) {
	state, err := loadRules("../prompts")
	if err != nil {
		t.Fatalf("loadRules: %v", err)
	}
	rw := &Rewriter{dir: "../prompts"}
	rw.state.Store(state)

	body := map[string]any{
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": []any{map[string]any{"type": "text", "text": attributionReminder}},
			},
		},
	}
	raw, _ := json.Marshal(body)
	out, _ := rw.Rewrite(raw)

	var parsed struct {
		Messages []struct {
			Content []map[string]string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.Messages[0].Content) != 1 {
		t.Fatalf("expected the sole block to be kept, got %d blocks", len(parsed.Messages[0].Content))
	}
}
