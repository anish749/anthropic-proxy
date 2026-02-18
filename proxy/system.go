package proxy

import "encoding/json"

var _ Extractor = (*SystemExtractor)(nil)

type SystemExtractor struct{}

func (SystemExtractor) Name() string { return "system" }

func (SystemExtractor) Extract(body map[string]json.RawMessage) (json.RawMessage, bool) {
	raw, ok := body["system"]
	return raw, ok
}
