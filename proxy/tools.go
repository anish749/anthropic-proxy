package proxy

import "encoding/json"

type ToolsExtractor struct{}

func (ToolsExtractor) Name() string { return "tools" }

func (ToolsExtractor) Extract(body map[string]json.RawMessage) (json.RawMessage, bool) {
	raw, ok := body["tools"]
	return raw, ok
}
