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
// The Type field determines whether a rule targets system prompt blocks or tool descriptions.
type replacementRule struct {
	Type      string `yaml:"type"`       // "system" or "tool"
	Block     int    `yaml:"block"`      // system: which prompt block to target
	Tool      string `yaml:"tool"`       // tool: which tool name to target
	Find      string `yaml:"find"`
	Replace   string `yaml:"replace"`
	Regex     bool   `yaml:"regex"`      // tool: treat Find as a regex
	Disabled  bool   `yaml:"disabled"`
	WarnAfter int    `yaml:"warn_after"` // warn if matched 0 times after N evaluations
}

// rule is the internal representation of a find-replace rule (system or tool).
// Stored as pointers so atomic stats counters work correctly.
type rule struct {
	// Type discriminator — only one of Block/Tool is meaningful per rule.
	Block int    // system: which prompt block to target
	Tool  string // tool: which tool name to target

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

	if r.Tool != "" {
		kind := ""
		if r.Regex {
			kind = " (regex)"
		}
		return fmt.Sprintf("tool %q%s: %q", r.Tool, kind, find)
	}
	return fmt.Sprintf("system block %d: %q", r.Block, find)
}

type Rewriter struct {
	// fullReplace maps block index → replacement text
	fullReplace map[int]string
	// systemRules maps block index → list of find/replace rules
	systemRules map[int][]*rule
	// toolRules maps tool name → list of rules
	toolRules map[string][]*rule

	reqCount atomic.Int64
}

func NewRewriter(dir string) *Rewriter {
	rw := &Rewriter{
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
		rw.fullReplace[i] = string(data)
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
			slog.Error("rewriter: failed to parse file", "path", path, "err", err)
			os.Exit(1)
		}

		sysLoaded, toolLoaded, skipped := 0, 0, 0
		for _, r := range rules {
			if r.Disabled {
				skipped++
				continue
			}
			switch r.Type {
			case "system":
				rw.systemRules[r.Block] = append(rw.systemRules[r.Block], &rule{
					Block: r.Block, Find: r.Find, Replace: r.Replace,
					WarnAfter: r.WarnAfter,
				})
				sysLoaded++
			case "tool":
				tr := &rule{
					Tool: r.Tool, Find: r.Find, Replace: r.Replace,
					Regex: r.Regex, WarnAfter: r.WarnAfter,
				}
				if r.Regex {
					re, err := regexp.Compile(r.Find)
					if err != nil {
						slog.Error("rewriter: invalid regex in rule", "tool", r.Tool, "err", err)
						os.Exit(1)
					}
					tr.re = re
				}
				rw.toolRules[r.Tool] = append(rw.toolRules[r.Tool], tr)
				toolLoaded++
			default:
				slog.Warn("rewriter: unknown rule type, skipping", "type", r.Type, "path", path)
			}
		}
		slog.Info("rewriter: loaded rules", "path", path, "system", sysLoaded, "tool", toolLoaded, "disabled", skipped)
	}

	return rw
}

func (rw *Rewriter) hasSystemRules() bool {
	return len(rw.fullReplace) > 0 || len(rw.systemRules) > 0
}

func (rw *Rewriter) Rewrite(body []byte) []byte {
	if !rw.hasSystemRules() && len(rw.toolRules) == 0 {
		return body
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		return body
	}

	modified := false

	// Rewrite system prompt blocks
	if rw.hasSystemRules() {
		if systemRaw, ok := parsed["system"]; ok {
			if newSystem, changed := rw.rewriteSystem(systemRaw); changed {
				parsed["system"] = newSystem
				modified = true
			}
		}
	}

	// Rewrite tool descriptions
	if len(rw.toolRules) > 0 {
		if toolsRaw, ok := parsed["tools"]; ok {
			if newTools, changed := rw.rewriteTools(toolsRaw); changed {
				parsed["tools"] = newTools
				modified = true
			}
		}
	}

	if !modified {
		slog.Debug("rewriter: no modifications applied")
		return body
	}

	newBody, err := json.Marshal(parsed)
	if err != nil {
		return body
	}
	slog.Info("rewriter: request rewritten")

	n := rw.reqCount.Add(1)
	rw.checkStats(n)

	return newBody
}

// allRules returns every rule across both system and tool maps.
func (rw *Rewriter) allRules() []*rule {
	var all []*rule
	for _, rules := range rw.systemRules {
		all = append(all, rules...)
	}
	for _, rules := range rw.toolRules {
		all = append(all, rules...)
	}
	return all
}

// checkStats logs a stats summary every statsLogInterval requests, and warns
// immediately about any rule that has never matched despite enough evaluations.
func (rw *Rewriter) checkStats(reqCount int64) {
	all := rw.allRules()

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

func (rw *Rewriter) rewriteSystem(systemRaw json.RawMessage) (json.RawMessage, bool) {
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
		if replacement, ok := rw.fullReplace[i]; ok {
			text = replacement
			modified = true
		} else if rules, ok := rw.systemRules[i]; ok {
			for _, r := range rules {
				r.seen.Add(1)
				if strings.Contains(text, r.Find) {
					text = strings.ReplaceAll(text, r.Find, r.Replace)
					r.matched.Add(1)
					modified = true
				} else {
					slog.Warn("rewriter: system rule did not match", "block", i, "find", r.Find)
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

func (rw *Rewriter) rewriteTools(toolsRaw json.RawMessage) (json.RawMessage, bool) {
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

		rules, ok := rw.toolRules[name]
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
					slog.Warn("rewriter: tool rule (regex) did not match", "tool", name, "find", rule.Find)
				}
			} else {
				if strings.Contains(desc, rule.Find) {
					desc = strings.ReplaceAll(desc, rule.Find, rule.Replace)
					rule.matched.Add(1)
					modified = true
				} else {
					slog.Warn("rewriter: tool rule did not match", "tool", name, "find", rule.Find)
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
