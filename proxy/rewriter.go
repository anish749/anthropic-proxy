package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type findReplaceRule struct {
	Block    int    `yaml:"block"`
	Find     string `yaml:"find"`
	Replace  string `yaml:"replace"`
	Disabled bool   `yaml:"disabled"`
}

type toolReplaceRule struct {
	Tool     string `yaml:"tool"`
	Find     string `yaml:"find"`
	Replace  string `yaml:"replace"`
	Disabled bool   `yaml:"disabled"`
}

type Rewriter struct {
	// fullReplace maps block index → replacement text
	fullReplace map[int]string
	// findReplace maps block index → list of find/replace rules
	findReplace map[int][]findReplaceRule
	// toolReplace maps tool name → list of find/replace rules
	toolReplace map[string][]toolReplaceRule
}

func NewRewriter(dir string) *Rewriter {
	rw := &Rewriter{
		fullReplace: make(map[int]string),
		findReplace: make(map[int][]findReplaceRule),
		toolReplace: make(map[string][]toolReplaceRule),
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
	yamlPath := filepath.Join(dir, "replacements.yaml")
	data, err := os.ReadFile(yamlPath)
	if err == nil {
		var rules []findReplaceRule
		if err := yaml.Unmarshal(data, &rules); err != nil {
			log.Printf("rewriter: failed to parse %s: %v", yamlPath, err)
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
	toolYamlPath := filepath.Join(dir, "tool_replacements.yaml")
	toolData, err := os.ReadFile(toolYamlPath)
	if err == nil {
		var rules []toolReplaceRule
		if err := yaml.Unmarshal(toolData, &rules); err != nil {
			log.Printf("rewriter: failed to parse %s: %v", toolYamlPath, err)
		} else {
			loaded := 0
			skipped := 0
			for _, r := range rules {
				if r.Disabled {
					skipped++
					continue
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
	return newBody
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
			if strings.Contains(desc, rule.Find) {
				desc = strings.ReplaceAll(desc, rule.Find, rule.Replace)
				modified = true
			} else {
				log.Printf("WARN: tool replacement rule for %q did not match: %q", name, rule.Find)
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
