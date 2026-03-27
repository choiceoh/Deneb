// Package liteparse wraps the LiteParse CLI (lit) for local document parsing.
//
// LiteParse extracts text from PDFs, Office documents (DOCX, XLSX, PPTX),
// OpenDocument formats, and images (via OCR). It runs entirely locally with
// no external API calls.
//
// Install: npm i -g @llamaindex/liteparse
// Docs:    https://github.com/run-llama/liteparse
package liteparse

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// maxOutputBytes caps the text output from a single parse to avoid flooding
// the LLM context window.
const maxOutputBytes = 200 * 1024 // 200 KB

// maxDocumentSize is the largest file we'll attempt to parse (50 MB).
const maxDocumentSize = 50 * 1024 * 1024

// parseTimeout is the per-parse execution timeout.
const parseTimeout = 60 * time.Second

// Cached availability check.
var (
	availableOnce sync.Once
	availableVal  bool
)

// Available returns true if the lit CLI is installed and reachable on PATH.
// The result is cached for the process lifetime.
func Available() bool {
	availableOnce.Do(func() {
		_, err := exec.LookPath("lit")
		availableVal = err == nil
	})
	return availableVal
}

// supportedMIMEPrefixes lists MIME type prefixes that LiteParse can handle.
var supportedMIMEPrefixes = []string{
	"application/pdf",
	// Office Open XML (DOCX, XLSX, PPTX)
	"application/vnd.openxmlformats-officedocument.",
	// Legacy Office (DOC, XLS, PPT)
	"application/msword",
	"application/vnd.ms-excel",
	"application/vnd.ms-powerpoint",
	// OpenDocument (ODT, ODS, ODP)
	"application/vnd.oasis.opendocument.",
	// CSV
	"text/csv",
}

// SupportedMIME returns true if the given MIME type is parseable by LiteParse.
func SupportedMIME(mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	for _, prefix := range supportedMIMEPrefixes {
		if strings.HasPrefix(mime, prefix) {
			return true
		}
	}
	return false
}

// Parse extracts text content from a document using the lit CLI.
// fileName is used to determine the temp file extension (important for format
// detection). Returns the extracted plain text.
func Parse(ctx context.Context, data []byte, fileName string) (string, error) {
	if !Available() {
		return "", fmt.Errorf("lit CLI not found; install with: npm i -g @llamaindex/liteparse")
	}
	if len(data) == 0 {
		return "", fmt.Errorf("empty document data")
	}
	if len(data) > maxDocumentSize {
		return "", fmt.Errorf("document too large (%d bytes, max %d)", len(data), maxDocumentSize)
	}

	// Create temp directory for input file.
	tmpDir, err := os.MkdirTemp("", "deneb-liteparse-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Preserve the original file extension for format detection.
	name := "input"
	if ext := filepath.Ext(fileName); ext != "" {
		name = "input" + ext
	}
	inputPath := filepath.Join(tmpDir, name)
	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}

	// Run lit parse with timeout.
	parseCtx, cancel := context.WithTimeout(ctx, parseTimeout)
	defer cancel()

	cmd := exec.CommandContext(parseCtx, "lit", "parse", inputPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("lit parse failed: %s", errMsg)
	}

	text := stdout.String()

	// Truncate if too large.
	if len(text) > maxOutputBytes {
		text = text[:maxOutputBytes] + "\n\n[... 텍스트가 너무 길어 잘렸습니다]"
	}

	return strings.TrimSpace(text), nil
}
