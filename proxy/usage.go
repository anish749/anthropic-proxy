package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

var _ Extractor = (*UsageExtractor)(nil)
var _ RawExtractor = (*UsageExtractor)(nil)

type UsageExtractor struct{}

func (UsageExtractor) Name() string { return "usage" }

func (UsageExtractor) Extract(body map[string]json.RawMessage) (json.RawMessage, bool) {
	raw, ok := body["usage"]
	return raw, ok
}

// ExtractFromRaw parses an SSE stream to collect usage data.
// The Anthropic streaming API splits usage across events:
//   - message_start contains input_tokens (in message.usage)
//   - message_delta contains output_tokens (in usage)
func (UsageExtractor) ExtractFromRaw(body []byte) (json.RawMessage, bool) {
	usage := make(map[string]any)

	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[len("data: "):]

		var event map[string]json.RawMessage
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}

		// message_start -> message -> usage
		if msgRaw, ok := event["message"]; ok {
			var msg map[string]json.RawMessage
			if json.Unmarshal(msgRaw, &msg) == nil {
				if u, ok := msg["usage"]; ok {
					var fields map[string]any
					if json.Unmarshal(u, &fields) == nil {
						for k, v := range fields {
							usage[k] = v
						}
					}
				}
			}
		}

		// message_delta -> usage
		if u, ok := event["usage"]; ok {
			var fields map[string]any
			if json.Unmarshal(u, &fields) == nil {
				for k, v := range fields {
					usage[k] = v
				}
			}
		}
	}

	if len(usage) == 0 {
		return nil, false
	}

	merged, err := json.Marshal(usage)
	if err != nil {
		return nil, false
	}
	return merged, true
}
