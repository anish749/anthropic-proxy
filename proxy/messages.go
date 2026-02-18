package proxy

import "encoding/json"

var _ Extractor = (*MessagesExtractor)(nil)

type MessagesExtractor struct{}

func (MessagesExtractor) Name() string { return "messages" }

func (MessagesExtractor) Extract(body map[string]json.RawMessage) (json.RawMessage, bool) {
	raw, ok := body["messages"]
	return raw, ok
}
