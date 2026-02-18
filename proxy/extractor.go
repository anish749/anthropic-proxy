package proxy

import "encoding/json"

// Extractor pulls a named part from a parsed Anthropic request body.
// Each implementation is responsible for a single field (tools, messages, etc.).
type Extractor interface {
	Name() string
	Extract(body map[string]json.RawMessage) (json.RawMessage, bool)
}
