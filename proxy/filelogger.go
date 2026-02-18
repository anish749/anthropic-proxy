package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

type FileLogger struct {
	dir        string
	extractors []Extractor
}

func NewFileLogger(dir string, extractors ...Extractor) *FileLogger {
	return &FileLogger{dir: dir, extractors: extractors}
}

func (fl *FileLogger) Log(requestID string, body []byte) {
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		return
	}

	if err := os.MkdirAll(fl.dir, 0o755); err != nil {
		log.Printf("failed to create requests dir: %v", err)
		return
	}

	for _, ext := range fl.extractors {
		raw, ok := ext.Extract(parsed)
		if !ok {
			continue
		}

		var buf bytes.Buffer
		if err := json.Indent(&buf, raw, "", "  "); err != nil {
			continue
		}

		filename := fmt.Sprintf("%s-%s.json", requestID, ext.Name())
		path := filepath.Join(fl.dir, filename)
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			log.Printf("failed to write %s: %v", path, err)
		}
	}
}
