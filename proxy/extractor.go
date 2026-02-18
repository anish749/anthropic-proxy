package proxy

import "encoding/json"

// Extractor pulls a named part from a parsed Anthropic request body.
// Each implementation is responsible for a single field (tools, messages, etc.).
type Extractor interface {
	Name() string
	Extract(body map[string]json.RawMessage) (json.RawMessage, bool)
}

// RawExtractor is an optional interface for extractors that can work
// directly on raw response bytes (e.g. SSE streams) when standard
// JSON parsing fails.
type RawExtractor interface {
	Extractor
	ExtractFromRaw(body []byte) (json.RawMessage, bool)
}
