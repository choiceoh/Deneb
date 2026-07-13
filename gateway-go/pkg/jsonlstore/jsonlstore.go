// Package jsonlstore provides JSONL (JSON Lines) file persistence
// with atomic snapshots and append-only logging.
//
// Designed for small-to-medium datasets that fit in memory (single-user).
// Uses stdlib only — zero external dependencies.
package jsonlstore

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// maxLineBytes bounds a single JSONL line. A longer line is treated as
// corruption and skipped (see Load) rather than read wholesale into memory.
const maxLineBytes = 1 << 20 // 1MB

// Load reads a JSONL file and decodes each line into T.
// Blank lines, corrupt lines, and oversize (>maxLineBytes) lines are skipped
// (crash recovery). Skipping is per-line: one bad line never aborts the scan
// or poisons the records around it. This matters where a single shared JSONL
// backs many logical streams — e.g. genesis skill_validation_cases.jsonl, whose
// held-out gate fails CLOSED on a read error, so one corrupt line would
// otherwise silently freeze evolution for every skill (RSI 4th-review M2-#3).
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
	reader := bufio.NewReader(f)
	for {
		line, over, err := readBoundedLine(reader, maxLineBytes)
		// A non-EOF read error (e.g. I/O failure mid-line) means the bytes
		// returned for THIS line may be a truncated fragment — do not decode
		// them (matching the old bufio.Scanner, which never yielded a partial
		// token on a read error). Surface the error with whatever we already
		// have. A clean io.EOF, by contrast, hands back a legitimate final
		// unterminated record, which we still decode below.
		if err != nil && !errors.Is(err, io.EOF) {
			return items, fmt.Errorf("jsonlstore: read %s: %w", path, err)
		}
		// An oversize line (over) is dropped: only up to maxLineBytes is ever
		// buffered before oversize is detected, and the unbounded remainder is
		// drained without buffering — so a torn/merged/externally-corrupted
		// giant line stays bounded in memory and does not stop the scan.
		if len(line) > 0 && !over {
			var item T
			if uErr := json.Unmarshal(line, &item); uErr == nil {
				items = append(items, item)
			}
			// Corrupt line (unmarshal failed) — skipped (partial writes).
		}
		if err != nil { // io.EOF: the final record has been handled above.
			return items, nil
		}
	}
}

// readBoundedLine reads one '\n'-terminated line, dropping the trailing newline.
// A line whose length would exceed max is not buffered: its bytes are drained up
// to the next newline and (nil, true, nil) is returned so the caller skips it
// and continues with the next line. The returned error is io.EOF once the reader
// is exhausted (with any final unterminated line returned alongside it).
func readBoundedLine(r *bufio.Reader, max int) (line []byte, over bool, err error) {
	var buf []byte
	oversize := false
	for {
		b, rerr := r.ReadByte()
		if rerr != nil {
			if oversize {
				return nil, true, rerr
			}
			return buf, false, rerr
		}
		if b == '\n' {
			return buf, oversize, nil
		}
		if oversize {
			continue // draining the remainder of an oversize line
		}
		if len(buf) >= max {
			oversize = true
			buf = nil // release; keep draining to the newline
			continue
		}
		buf = append(buf, b)
	}
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
