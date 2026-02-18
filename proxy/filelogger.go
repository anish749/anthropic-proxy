package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

type FileLogger struct {
	dir            string
	reqExtractors  []Extractor
	respExtractors []Extractor
}

func NewFileLogger(dir string, reqExtractors []Extractor, respExtractors []Extractor) *FileLogger {
	return &FileLogger{dir: dir, reqExtractors: reqExtractors, respExtractors: respExtractors}
}

func (fl *FileLogger) Log(requestID string, reqBody, respBody []byte) {
	var parsedReq map[string]json.RawMessage
	if err := json.Unmarshal(reqBody, &parsedReq); err != nil {
		return
	}

	if err := os.MkdirAll(fl.dir, 0o755); err != nil {
		log.Printf("failed to create requests dir: %v", err)
		return
	}

	model := extractModel(parsedReq)
	ts := time.Now().UTC().Format("20060102-150405")
	prefix := fmt.Sprintf("%s-%s-%s", ts, requestID, model)

	fl.writeExtracted(prefix, parsedReq, fl.reqExtractors)
	fl.writeExtractedRaw(prefix, respBody, fl.respExtractors)

	fmt.Printf("logged request %s (model=%s)\n", requestID, model)
}

func (fl *FileLogger) writeExtracted(prefix string, parsed map[string]json.RawMessage, extractors []Extractor) {
	for _, ext := range extractors {
		raw, ok := ext.Extract(parsed)
		if !ok {
			continue
		}
		fl.writeFile(prefix, ext.Name(), raw)
	}
}

func (fl *FileLogger) writeExtractedRaw(prefix string, body []byte, extractors []Extractor) {
	// Try as plain JSON first
	var parsed map[string]json.RawMessage
	jsonOK := json.Unmarshal(body, &parsed) == nil

	for _, ext := range extractors {
		var raw json.RawMessage
		var ok bool

		if jsonOK {
			raw, ok = ext.Extract(parsed)
		}

		// Fall back to RawExtractor if JSON parsing failed or key wasn't found
		if !ok {
			if re, isRaw := ext.(RawExtractor); isRaw {
				raw, ok = re.ExtractFromRaw(body)
			}
		}

		if !ok {
			continue
		}
		fl.writeFile(prefix, ext.Name(), raw)
	}
}

func (fl *FileLogger) writeFile(prefix, name string, raw json.RawMessage) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return
	}

	filename := fmt.Sprintf("%s-%s.json", prefix, name)
	path := filepath.Join(fl.dir, filename)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		log.Printf("failed to write %s: %v", path, err)
	}
}

func extractModel(parsed map[string]json.RawMessage) string {
	raw, ok := parsed["model"]
	if !ok {
		return "unknown"
	}
	var model string
	if err := json.Unmarshal(raw, &model); err != nil {
		return "unknown"
	}
	return model
}
