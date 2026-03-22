package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type FileLogger struct {
	dir            string
	format         string // "json" or "yaml"
	reqExtractors  []Extractor
	respExtractors []Extractor
}

func NewFileLogger(dir string, format string, reqExtractors []Extractor, respExtractors []Extractor) *FileLogger {
	if format == "" {
		format = "yaml"
	}
	return &FileLogger{dir: dir, format: format, reqExtractors: reqExtractors, respExtractors: respExtractors}
}

func (fl *FileLogger) Log(requestID string, reqBody, respBody []byte) {
	var parsedReq map[string]json.RawMessage
	if err := json.Unmarshal(reqBody, &parsedReq); err != nil {
		return
	}

	if err := os.MkdirAll(fl.dir, 0o755); err != nil {
		slog.Error("failed to create requests dir", "err", err)
		return
	}

	model := extractModel(parsedReq)
	ts := time.Now().UTC().Format("20060102-150405")
	prefix := fmt.Sprintf("%s-%s-%s", ts, requestID, model)

	fl.writeExtracted(prefix, parsedReq, fl.reqExtractors)
	fl.writeExtractedRaw(prefix, respBody, fl.respExtractors)

	slog.Info("logged request", "id", requestID, "model", model)
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
	var data []byte
	var ext string

	switch fl.format {
	case "yaml":
		var obj any
		if err := json.Unmarshal(raw, &obj); err != nil {
			slog.Error("failed to parse JSON for YAML conversion", "err", err)
			return
		}
		out, err := yaml.Marshal(obj)
		if err != nil {
			slog.Error("failed to marshal YAML", "err", err)
			return
		}
		data = out
		ext = "yaml"
	default:
		var buf bytes.Buffer
		if err := json.Indent(&buf, raw, "", "  "); err != nil {
			return
		}
		data = buf.Bytes()
		ext = "json"
	}

	filename := fmt.Sprintf("%s-%s.%s", prefix, name, ext)
	path := filepath.Join(fl.dir, filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		slog.Error("failed to write file", "path", path, "err", err)
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
