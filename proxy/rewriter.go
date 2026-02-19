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

type Rewriter struct {
	// fullReplace maps block index → replacement text
	fullReplace map[int]string
	// findReplace maps block index → list of find/replace rules
	findReplace map[int][]findReplaceRule
}

func NewRewriter(dir string) *Rewriter {
	rw := &Rewriter{
		fullReplace: make(map[int]string),
		findReplace: make(map[int][]findReplaceRule),
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

	return rw
}

func (rw *Rewriter) Rewrite(body []byte) []byte {
	if len(rw.fullReplace) == 0 && len(rw.findReplace) == 0 {
		return body
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		return body
	}

	systemRaw, ok := parsed["system"]
	if !ok {
		return body
	}

	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(systemRaw, &blocks); err != nil {
		return body
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
		log.Println("rewriter: no modifications applied")
		return body
	}

	newSystem, err := json.Marshal(blocks)
	if err != nil {
		return body
	}
	parsed["system"] = newSystem

	newBody, err := json.Marshal(parsed)
	if err != nil {
		return body
	}
	log.Println("rewriter: system prompt rewritten")
	return newBody
}
