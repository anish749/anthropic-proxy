package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

// extractFromTarGz extracts a single file by name from a tar.gz archive in memory.
func extractFromTarGz(data []byte, filename string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip open: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar read: %w", err)
		}
		if hdr.Name == filename {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("file %q not found in archive", filename)
}
