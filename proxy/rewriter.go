package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

const (
	systemReplacementsFile = "replacements.yaml"
	toolReplacementsFile   = "tool_replacements.yaml"

	// Log a stats summary every statsLogInterval requests.
	statsLogInterval = 50
)

type findReplaceRule struct {
	Block    int    `yaml:"block"`
	Find     string `yaml:"find"`
	Replace  string `yaml:"replace"`
	Disabled bool   `yaml:"disabled"`
}

type toolReplaceRule struct {
	Tool      string `yaml:"tool"`
	Find      string `yaml:"find"`
	Replace   string `yaml:"replace"`
	Regex     bool   `yaml:"regex"`
	Disabled  bool   `yaml:"disabled"`
	WarnAfter int    `yaml:"warn_after"` // warn if matched 0 times after this many evaluations (0 = never warn)
	re        *regexp.Regexp              // compiled from Find when Regex: true

	// match stats — updated atomically, never copied (rules stored as pointers)
	seen    atomic.Int64
	matched atomic.Int64
}

// label returns a short human-readable identifier for log output.
func (r *toolReplaceRule) label() string {
	find := r.Find
	if len(find) > 48 {
		find = find[:48] + "…"
	}
	find = strings.ReplaceAll(find, "\n", "↵")
	kind := ""
	if r.Regex {
		kind = " (regex)"
	}
	return fmt.Sprintf("%q%s: %q", r.Tool, kind, find)
}

type Rewriter struct {
	// fullReplace maps block index → replacement text
	fullReplace map[int]string
	// findReplace maps block index → list of find/replace rules
	findReplace map[int][]findReplaceRule
	// toolReplace maps tool name → list of rules (pointers for atomic stats)
	toolReplace map[string][]*toolReplaceRule

	reqCount atomic.Int64
}

func NewRewriter(dir string) *Rewriter {
	rw := &Rewriter{
		fullReplace: make(map[int]string),
		findReplace: make(map[int][]findReplaceRule),
		toolReplace: make(map[string][]*toolReplaceRule),
	}

	// Load full replacement files: system-{i}-replace.txt
	for i := 0; i < 10; i++ {
		path := filepath.Join(dir, fmt.Sprintf("system-%d-replace.txt", i))
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		rw.fullReplace[i] = string(data)
		log.Printf("rewriter: loaded full replacement for block %d from %s", i, path)
	}

	// Load find-and-replace rules from replacements.yaml
	yamlPath := filepath.Join(dir, systemReplacementsFile)
	data, err := os.ReadFile(yamlPath)
	if err == nil {
		var rules []findReplaceRule
		if err := yaml.Unmarshal(data, &rules); err != nil {
			log.Fatalf("rewriter: failed to parse %s: %v", yamlPath, err)
		} else {
			loaded := 0
			skipped := 0
			for _, r := range rules {
				if r.Disabled {
					skipped++
					continue
				}
				rw.findReplace[r.Block] = append(rw.findReplace[r.Block], r)
				loaded++
			}
			log.Printf("rewriter: loaded %d find-replace rules from %s (%d disabled)", loaded, yamlPath, skipped)
		}
	}

	// Load tool description find-and-replace rules from tool_replacements.yaml
	toolYamlPath := filepath.Join(dir, toolReplacementsFile)
	toolData, err := os.ReadFile(toolYamlPath)
	if err == nil {
		var rules []toolReplaceRule
		if err := yaml.Unmarshal(toolData, &rules); err != nil {
			log.Fatalf("rewriter: failed to parse %s: %v", toolYamlPath, err)
		} else {
			loaded := 0
			skipped := 0
			for i := range rules {
				r := &rules[i]
				if r.Disabled {
					skipped++
					continue
				}
				if r.Regex {
					re, err := regexp.Compile(r.Find)
					if err != nil {
						log.Fatalf("rewriter: invalid regex in tool replacement rule for %q: %v", r.Tool, err)
					}
					r.re = re
				}
				rw.toolReplace[r.Tool] = append(rw.toolReplace[r.Tool], r)
				loaded++
			}
			log.Printf("rewriter: loaded %d tool replacement rules from %s (%d disabled)", loaded, toolYamlPath, skipped)
		}
	}

	return rw
}

func (rw *Rewriter) hasSystemRules() bool {
	return len(rw.fullReplace) > 0 || len(rw.findReplace) > 0
}

func (rw *Rewriter) Rewrite(body []byte) []byte {
	if !rw.hasSystemRules() && len(rw.toolReplace) == 0 {
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
	if len(rw.toolReplace) > 0 {
		if toolsRaw, ok := parsed["tools"]; ok {
			if newTools, changed := rw.rewriteTools(toolsRaw); changed {
				parsed["tools"] = newTools
				modified = true
			}
		}
	}

	if !modified {
		log.Println("rewriter: no modifications applied")
		return body
	}

	newBody, err := json.Marshal(parsed)
	if err != nil {
		return body
	}
	log.Println("rewriter: request rewritten")

	n := rw.reqCount.Add(1)
	rw.checkStats(n)

	return newBody
}

// checkStats logs a stats summary every statsLogInterval requests, and warns
// immediately about any rule that has never matched despite enough evaluations.
func (rw *Rewriter) checkStats(reqCount int64) {
	// Per-request: warn about zero-match rules that have crossed their threshold.
	for _, rules := range rw.toolReplace {
		for _, r := range rules {
			if r.WarnAfter <= 0 {
				continue
			}
			seen := r.seen.Load()
			if seen >= int64(r.WarnAfter) && r.matched.Load() == 0 {
				log.Printf("WARN: tool rule never matched after %d evaluations — may need updating: %s", seen, r.label())
			}
		}
	}

	// Periodic: full summary every statsLogInterval requests.
	if reqCount%statsLogInterval != 0 {
		return
	}
	log.Printf("rewriter: stats after %d requests:", reqCount)
	for _, rules := range rw.toolReplace {
		for _, r := range rules {
			seen := r.seen.Load()
			matched := r.matched.Load()
			if seen == 0 {
				log.Printf("  [no data]  %s", r.label())
				continue
			}
			pct := float64(matched) / float64(seen) * 100
			flag := ""
			if matched == 0 {
				flag = " ← NEVER MATCHED"
			} else if pct < 50 {
				flag = " ← low match rate"
			}
			log.Printf("  matched %d/%d (%.0f%%)  %s%s", matched, seen, pct, r.label(), flag)
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
		} else if rules, ok := rw.findReplace[i]; ok {
			for _, rule := range rules {
				if strings.Contains(text, rule.Find) {
					text = strings.ReplaceAll(text, rule.Find, rule.Replace)
					modified = true
				} else {
					log.Printf("WARN: replacement rule for block %d did not match: %q", i, rule.Find)
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
	log.Println("rewriter: system prompt rewritten")
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

		rules, ok := rw.toolReplace[name]
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
					log.Printf("WARN: tool replacement rule (regex) for %q did not match: %q", name, rule.Find)
				}
			} else {
				if strings.Contains(desc, rule.Find) {
					desc = strings.ReplaceAll(desc, rule.Find, rule.Replace)
					rule.matched.Add(1)
					modified = true
				} else {
					log.Printf("WARN: tool replacement rule for %q did not match: %q", name, rule.Find)
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
	log.Printf("rewriter: tool descriptions rewritten")
	return newTools, true
}
