package proxy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

const (
	// Log a stats summary every statsLogInterval requests.
	statsLogInterval = 50
)

// replacementRule is the unified YAML schema for all replacement rules.
// The Type field determines whether a rule targets system prompt blocks,
// tool descriptions, or system-reminder blocks in user messages.
type replacementRule struct {
	Type      string `yaml:"type"`       // "system", "tool", or "system-reminder"
	Block     int    `yaml:"block"`      // system: which prompt block to target
	Tool      string `yaml:"tool"`       // tool: which tool name to target
	Find      string `yaml:"find"`
	Replace   string `yaml:"replace"`
	Regex     bool   `yaml:"regex"`      // treat Find as a regex
	Disabled  bool   `yaml:"disabled"`
	WarnAfter int    `yaml:"warn_after"` // warn if matched 0 times after N evaluations
}

// rule is the internal representation of a find-replace rule (system, tool, or system-reminder).
// Stored as pointers so atomic stats counters work correctly.
type rule struct {
	RuleType string // "system", "tool", or "system-reminder"
	Block    int    // system: which prompt block to target
	Tool     string // tool: which tool name to target

	Find      string
	Replace   string
	Regex     bool
	WarnAfter int
	re        *regexp.Regexp // compiled from Find when Regex is true

	// match stats — updated atomically, never copied
	seen    atomic.Int64
	matched atomic.Int64
}

// label returns a short human-readable identifier for log output.
func (r *rule) label() string {
	find := r.Find
	if len(find) > 48 {
		find = find[:48] + "…"
	}
	find = strings.ReplaceAll(find, "\n", "↵")

	kind := ""
	if r.Regex {
		kind = " (regex)"
	}

	switch r.RuleType {
	case "tool":
		return fmt.Sprintf("tool %q%s: %q", r.Tool, kind, find)
	case "system-reminder":
		return fmt.Sprintf("system-reminder%s: %q", kind, find)
	default:
		return fmt.Sprintf("system block %d%s: %q", r.Block, kind, find)
	}
}

// Warnings collects log messages during rewriting so they can be
// flushed later with the upstream request ID for correlation.
type Warnings struct {
	entries []logEntry
}

type logEntry struct {
	msg  string
	args []any
}

func (w *Warnings) add(msg string, args ...any) {
	w.entries = append(w.entries, logEntry{msg, args})
}

// HasWarnings reports whether any warnings were collected.
func (w *Warnings) HasWarnings() bool {
	return len(w.entries) > 0
}

// Flush logs all collected warnings. If reqID is non-empty, it is
// appended to each log line so warnings can be correlated with
// logged request files.
func (w *Warnings) Flush(reqID string) {
	for _, e := range w.entries {
		args := e.args
		if reqID != "" {
			args = append(args, "req", reqID)
		}
		slog.Warn(e.msg, args...)
	}
}

// rewriterState holds the immutable rule set for a single load generation.
// Swapped atomically so in-flight requests see a consistent snapshot.
type rewriterState struct {
	fullReplace   map[int]string
	systemRules   map[int][]*rule
	toolRules     map[string][]*rule
	reminderRules []*rule
}

func (s *rewriterState) hasSystemRules() bool {
	return len(s.fullReplace) > 0 || len(s.systemRules) > 0
}

func (s *rewriterState) allRules() []*rule {
	var all []*rule
	for _, rules := range s.systemRules {
		all = append(all, rules...)
	}
	for _, rules := range s.toolRules {
		all = append(all, rules...)
	}
	all = append(all, s.reminderRules...)
	return all
}

type Rewriter struct {
	state    atomic.Pointer[rewriterState]
	dir      string
	reqCount atomic.Int64
	planMode *PlanModeRewriter
}

func NewRewriter(dir string) *Rewriter {
	rw := &Rewriter{dir: dir}
	state, err := loadRules(dir)
	if err != nil {
		slog.Error("rewriter: "+err.Error())
		os.Exit(1)
	}
	rw.state.Store(state)
	rw.planMode = NewPlanModeRewriter(filepath.Join(dir, "plan-mode.txt"))
	return rw
}

// loadRules reads all prompt files from dir and returns an immutable rewriterState.
// Returns an error for fatal parse failures; missing files are silently skipped.
func loadRules(dir string) (*rewriterState, error) {
	s := &rewriterState{
		fullReplace: make(map[int]string),
		systemRules: make(map[int][]*rule),
		toolRules:   make(map[string][]*rule),
	}

	// Load full replacement files: system-{i}-replace.txt
	for i := 0; i < 10; i++ {
		path := filepath.Join(dir, fmt.Sprintf("system-%d-replace.txt", i))
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		s.fullReplace[i] = string(data)
		slog.Info("rewriter: loaded full replacement", "block", i, "path", path)
	}

	// Load all *.yaml files from the directory
	files, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rules []replacementRule
		if err := yaml.Unmarshal(data, &rules); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}

		sysLoaded, toolLoaded, reminderLoaded, skipped := 0, 0, 0, 0
		for _, r := range rules {
			if r.Disabled {
				skipped++
				continue
			}

			// Compile regex if needed (shared by tool and system-reminder rules)
			var compiled *regexp.Regexp
			if r.Regex {
				re, err := regexp.Compile(r.Find)
				if err != nil {
					return nil, fmt.Errorf("invalid regex in %s rule: %w", r.Type, err)
				}
				compiled = re
			}

			switch r.Type {
			case "system":
				s.systemRules[r.Block] = append(s.systemRules[r.Block], &rule{
					RuleType: "system", Block: r.Block, Find: r.Find, Replace: r.Replace,
					Regex: r.Regex, re: compiled, WarnAfter: r.WarnAfter,
				})
				sysLoaded++
			case "tool":
				s.toolRules[r.Tool] = append(s.toolRules[r.Tool], &rule{
					RuleType: "tool", Tool: r.Tool, Find: r.Find, Replace: r.Replace,
					Regex: r.Regex, re: compiled, WarnAfter: r.WarnAfter,
				})
				toolLoaded++
			case "system-reminder":
				s.reminderRules = append(s.reminderRules, &rule{
					RuleType: "system-reminder", Find: r.Find, Replace: r.Replace,
					Regex: r.Regex, re: compiled, WarnAfter: r.WarnAfter,
				})
				reminderLoaded++
			default:
				slog.Warn("rewriter: unknown rule type, skipping", "type", r.Type, "path", path)
			}
		}
		slog.Info("rewriter: loaded rules", "path", path, "system", sysLoaded, "tool", toolLoaded, "system-reminder", reminderLoaded, "disabled", skipped)
	}

	return s, nil
}


func (rw *Rewriter) Rewrite(body []byte) ([]byte, *Warnings) {
	w := &Warnings{}
	s := rw.state.Load()

	if !s.hasSystemRules() && len(s.toolRules) == 0 && len(s.reminderRules) == 0 && rw.planMode == nil {
		return body, w
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		return body, w
	}

	modified := false

	// Rewrite system prompt blocks
	if s.hasSystemRules() {
		if systemRaw, ok := parsed["system"]; ok {
			if newSystem, changed := rw.rewriteSystem(s, systemRaw, w); changed {
				parsed["system"] = newSystem
				modified = true
			}
		}
	}

	// Rewrite tool descriptions
	if len(s.toolRules) > 0 {
		if toolsRaw, ok := parsed["tools"]; ok {
			if newTools, changed := rw.rewriteTools(s, toolsRaw, w); changed {
				parsed["tools"] = newTools
				modified = true
			}
		}
	}

	// Rewrite system-reminder blocks in user messages (plan mode + regular rules)
	if len(s.reminderRules) > 0 || rw.planMode != nil {
		if msgsRaw, ok := parsed["messages"]; ok {
			if newMsgs, changed := rw.rewriteReminders(s, msgsRaw, w); changed {
				parsed["messages"] = newMsgs
				modified = true
			}
		}
	}

	if !modified {
		slog.Debug("rewriter: no modifications applied")
		return body, w
	}

	newBody, err := json.Marshal(parsed)
	if err != nil {
		return body, w
	}
	slog.Info("rewriter: request rewritten")

	n := rw.reqCount.Add(1)
	rw.checkStats(s, n)

	return newBody, w
}

// checkStats logs a stats summary every statsLogInterval requests, and warns
// immediately about any rule that has never matched despite enough evaluations.
func (rw *Rewriter) checkStats(s *rewriterState, reqCount int64) {
	all := s.allRules()

	// Per-request: warn about zero-match rules that have crossed their threshold.
	for _, r := range all {
		if r.WarnAfter <= 0 {
			continue
		}
		seen := r.seen.Load()
		if seen >= int64(r.WarnAfter) && r.matched.Load() == 0 {
			slog.Warn("rule never matched — may need updating", "evals", seen, "rule", r.label())
		}
	}

	// Periodic: full summary every statsLogInterval requests.
	if reqCount%statsLogInterval != 0 {
		return
	}
	slog.Info("rewriter: stats summary", "requests", reqCount)
	for _, r := range all {
		seen := r.seen.Load()
		matched := r.matched.Load()
		if seen == 0 {
			slog.Info("rewriter: rule stats", "status", "no data", "rule", r.label())
			continue
		}
		pct := float64(matched) / float64(seen) * 100
		flag := ""
		if matched == 0 {
			flag = "NEVER MATCHED"
		} else if pct < 50 {
			flag = "low match rate"
		}
		if flag != "" {
			slog.Warn("rewriter: rule stats", "matched", fmt.Sprintf("%d/%d (%.0f%%)", matched, seen, pct), "rule", r.label(), "flag", flag)
		} else {
			slog.Info("rewriter: rule stats", "matched", fmt.Sprintf("%d/%d (%.0f%%)", matched, seen, pct), "rule", r.label())
		}
	}
}

func (rw *Rewriter) rewriteSystem(s *rewriterState, systemRaw json.RawMessage, w *Warnings) (json.RawMessage, bool) {
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(systemRaw, &blocks); err != nil {
		return nil, false
	}

	modified := false
	for i, block := range blocks {
		textRaw, ok := block["text"]
		if !ok {
			continue
		}
		var text string
		if err := json.Unmarshal(textRaw, &text); err != nil {
			continue
		}

		// Full replacement takes precedence
		if replacement, ok := s.fullReplace[i]; ok {
			text = replacement
			modified = true
		} else if rules, ok := s.systemRules[i]; ok {
			for _, r := range rules {
				r.seen.Add(1)
				if strings.Contains(text, r.Find) {
					text = strings.ReplaceAll(text, r.Find, r.Replace)
					r.matched.Add(1)
					modified = true
				} else {
					w.add("rewriter: system rule did not match", "block", i, "rule", r.label())
				}
			}
		}

		if modified {
			newText, err := json.Marshal(text)
			if err != nil {
				continue
			}
			blocks[i]["text"] = newText
		}
	}

	if !modified {
		return nil, false
	}

	newSystem, err := json.Marshal(blocks)
	if err != nil {
		return nil, false
	}
	slog.Info("rewriter: system prompt rewritten")
	return newSystem, true
}

func (rw *Rewriter) rewriteTools(s *rewriterState, toolsRaw json.RawMessage, w *Warnings) (json.RawMessage, bool) {
	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(toolsRaw, &tools); err != nil {
		return nil, false
	}

	modified := false
	for i, tool := range tools {
		nameRaw, ok := tool["name"]
		if !ok {
			continue
		}
		var name string
		if err := json.Unmarshal(nameRaw, &name); err != nil {
			continue
		}

		rules, ok := s.toolRules[name]
		if !ok {
			continue
		}

		descRaw, ok := tool["description"]
		if !ok {
			continue
		}
		var desc string
		if err := json.Unmarshal(descRaw, &desc); err != nil {
			continue
		}

		for _, rule := range rules {
			rule.seen.Add(1)
			if rule.re != nil {
				if rule.re.MatchString(desc) {
					desc = rule.re.ReplaceAllString(desc, rule.Replace)
					rule.matched.Add(1)
					modified = true
				} else {
					w.add("rewriter: tool rule (regex) did not match", "tool", name, "rule", rule.label())
				}
			} else {
				if strings.Contains(desc, rule.Find) {
					desc = strings.ReplaceAll(desc, rule.Find, rule.Replace)
					rule.matched.Add(1)
					modified = true
				} else {
					w.add("rewriter: tool rule did not match", "tool", name, "rule", rule.label())
				}
			}
		}

		if modified {
			newDesc, err := json.Marshal(desc)
			if err != nil {
				continue
			}
			tools[i]["description"] = newDesc
		}
	}

	if !modified {
		return nil, false
	}

	newTools, err := json.Marshal(tools)
	if err != nil {
		return nil, false
	}
	slog.Info("rewriter: tool descriptions rewritten")
	return newTools, true
}

// systemReminderRe matches <system-reminder>...</system-reminder> tags in message text.
var rewriterReminderRe = regexp.MustCompile(`(?s)<system-reminder>\s*(.*?)\s*</system-reminder>`)

func (rw *Rewriter) rewriteReminders(s *rewriterState, msgsRaw json.RawMessage, w *Warnings) (json.RawMessage, bool) {
	var msgs []map[string]json.RawMessage
	if err := json.Unmarshal(msgsRaw, &msgs); err != nil {
		return nil, false
	}

	modified := false
	for i, msg := range msgs {
		roleRaw, ok := msg["role"]
		if !ok {
			continue
		}
		var role string
		if err := json.Unmarshal(roleRaw, &role); err != nil || role != "user" {
			continue
		}

		contentRaw, ok := msg["content"]
		if !ok {
			continue
		}

		// Content can be a string or an array of blocks.
		// Try string first, wrap in a single-element array for uniform handling.
		var blocks []map[string]json.RawMessage
		var plainStr string
		if err := json.Unmarshal(contentRaw, &plainStr); err == nil {
			textJSON, _ := json.Marshal(plainStr)
			typeJSON, _ := json.Marshal("text")
			blocks = []map[string]json.RawMessage{
				{"type": typeJSON, "text": textJSON},
			}
		} else if err := json.Unmarshal(contentRaw, &blocks); err != nil {
			continue
		}

		isStringContent := plainStr != ""
		contentModified := false
		for j, block := range blocks {
			typeRaw, ok := block["type"]
			if !ok {
				continue
			}
			var blockType string
			if err := json.Unmarshal(typeRaw, &blockType); err != nil {
				continue
			}

			// Determine which field holds the text: "text" for text blocks,
			// "content" for tool_result blocks (which can contain system-reminders).
			var textField string
			switch blockType {
			case "text":
				textField = "text"
			case "tool_result":
				textField = "content"
			default:
				continue
			}

			textRaw, ok := block[textField]
			if !ok {
				continue
			}
			var text string
			if err := json.Unmarshal(textRaw, &text); err != nil {
				continue
			}

			if !rewriterReminderRe.MatchString(text) {
				continue
			}

			// Apply rules to the content inside each <system-reminder> block
			newText := rewriterReminderRe.ReplaceAllStringFunc(text, func(match string) string {
				inner := rewriterReminderRe.FindStringSubmatch(match)
				if len(inner) < 2 {
					return match
				}
				content := inner[1]

				// Plan mode: full replacement of plan mode reminders
				if rw.planMode != nil {
					if replacement, ok := rw.planMode.Rewrite(content); ok {
						return "<system-reminder>\n" + replacement + "\n</system-reminder>"
					}
				}

				for _, r := range s.reminderRules {
					r.seen.Add(1)
					if r.re != nil {
						if r.re.MatchString(content) {
							content = r.re.ReplaceAllString(content, r.Replace)
							r.matched.Add(1)
						} else {
							w.add("rewriter: system-reminder rule (regex) did not match", "rule", r.label())
						}
					} else {
						if strings.Contains(content, r.Find) {
							content = strings.ReplaceAll(content, r.Find, r.Replace)
							r.matched.Add(1)
						} else {
							w.add("rewriter: system-reminder rule did not match", "rule", r.label())
						}
					}
				}

				// If content is empty after replacements, remove the entire block
				if strings.TrimSpace(content) == "" {
					return ""
				}
				return "<system-reminder>\n" + content + "\n</system-reminder>"
			})

			if newText != text {
				newTextJSON, err := json.Marshal(newText)
				if err != nil {
					continue
				}
				blocks[j][textField] = newTextJSON
				contentModified = true
			}
		}

		if contentModified {
			var newContent json.RawMessage
			var err error
			if isStringContent {
				// Preserve original string format
				var text string
				json.Unmarshal(blocks[0]["text"], &text)
				newContent, err = json.Marshal(text)
			} else {
				newContent, err = json.Marshal(blocks)
			}
			if err != nil {
				continue
			}
			msgs[i]["content"] = newContent
			modified = true
		}
	}

	if !modified {
		return nil, false
	}

	newMsgs, err := json.Marshal(msgs)
	if err != nil {
		return nil, false
	}
	slog.Info("rewriter: system-reminder blocks rewritten")
	return newMsgs, true
}
