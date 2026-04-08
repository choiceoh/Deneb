// Package jsonlstore provides JSONL (JSON Lines) file persistence
// with atomic snapshots and append-only logging.
//
// Designed for small-to-medium datasets that fit in memory (single-user).
// Uses stdlib only — zero external dependencies.
package jsonlstore

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// Load reads a JSONL file and decodes each line into T.
// Blank lines and corrupt trailing lines are skipped (crash recovery).
func Load[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("jsonlstore: open %s: %w", path, err)
	}
	defer f.Close()

	var items []T
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20) // 1MB max line
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var item T
		if err := json.Unmarshal(line, &item); err != nil {
			// Skip corrupt lines (crash recovery for partial writes).
			continue
		}
		items = append(items, item)
	}
	if err := scanner.Err(); err != nil {
		return items, fmt.Errorf("jsonlstore: scan %s: %w", path, err)
	}
	return items, nil
}

// Append writes a single JSON line to the end of a file.
// Creates the file and parent directories if they don't exist.
func Append[T any](path string, item T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("jsonlstore: mkdir: %w", err)
	}

	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("jsonlstore: marshal: %w", err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("jsonlstore: open %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("jsonlstore: write: %w", err)
	}
	return f.Close()
}

// Snapshot atomically writes all items as a JSONL file.
// Uses atomic write (temp + rename) for crash safety.
func Snapshot[T any](path string, items []T) error {
	var buf []byte
	for _, item := range items {
		line, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("jsonlstore: marshal: %w", err)
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	return atomicfile.WriteFile(path, buf, &atomicfile.Options{Fsync: true})
}
