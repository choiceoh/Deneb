package liteparse

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// resetAvailability isolates the package-level PATH cache between boundary
// cases. Tests in this file intentionally do not call t.Parallel because the
// production cache is process-global by design.
func resetAvailability(t *testing.T) {
	t.Helper()
	availableOnce = sync.Once{}
	availableVal = false
	t.Cleanup(func() {
		availableOnce = sync.Once{}
		availableVal = false
	})
}

func installBoundaryLit(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("boundary fake uses a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "lit")
	content := "#!/bin/sh\nset -eu\n" + script + "\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake lit: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	resetAvailability(t)
	return path
}

func TestBoundaryAvailableCachesPositiveLookup(t *testing.T) {
	path := installBoundaryLit(t, `printf 'ok'`)
	if !Available() {
		t.Fatal("Available() = false with executable lit on PATH")
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove fake lit: %v", err)
	}
	if !Available() {
		t.Fatal("positive availability result was not cached")
	}
}

func TestBoundaryAvailableCachesNegativeLookup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	resetAvailability(t)
	if Available() {
		t.Fatal("Available() = true with empty PATH")
	}

	path := filepath.Join(dir, "lit")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf late"), 0o755); err != nil {
		t.Fatal(err)
	}
	if Available() {
		t.Fatal("negative availability result was not cached")
	}
}

func TestBoundaryAvailableConcurrentFirstLookupIsStable(t *testing.T) {
	installBoundaryLit(t, `printf 'ok'`)
	const workers = 96
	start := make(chan struct{})
	results := make(chan bool, workers)
	for i := 0; i < workers; i++ {
		go func() {
			<-start
			results <- Available()
		}()
	}
	close(start)
	for i := 0; i < workers; i++ {
		if got := <-results; !got {
			t.Fatalf("worker %d observed unavailable", i)
		}
	}
}

func TestBoundaryParseAvailabilityPrecedesInputValidation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	resetAvailability(t)

	tests := []struct {
		name string
		data []byte
		file string
	}{
		{
			name: "nil document",
			data: nil,
			file: "nil.pdf",
		},
		{
			name: "empty document",
			data: []byte{},
			file: "empty.docx",
		},
		{
			name: "ordinary document",
			data: []byte("content"),
			file: "ordinary.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(context.Background(), tt.data, tt.file)
			if err == nil || !strings.Contains(err.Error(), "lit CLI not found") {
				t.Fatalf("error = %v, want availability error", err)
			}
		})
	}
}

func TestBoundaryParseEmptyInputMatrix(t *testing.T) {
	installBoundaryLit(t, `printf 'should-not-run'; exit 91`)
	tests := []struct {
		name string
		data []byte
		file string
	}{
		{
			name: "nil bytes",
			data: nil,
			file: "doc.pdf",
		},
		{
			name: "zero length allocation",
			data: make([]byte, 0),
			file: "doc.docx",
		},
		{
			name: "zero length with no filename",
			data: []byte{},
			file: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(context.Background(), tt.data, tt.file)
			if err == nil || err.Error() != "empty document data" {
				t.Fatalf("Parse() output=%q error=%v", got, err)
			}
			if got != "" {
				t.Fatalf("empty input returned output %q", got)
			}
		})
	}
}

func TestBoundaryParseMaximumDocumentSize(t *testing.T) {
	installBoundaryLit(t, `printf '%s' "$(wc -c < "$2" | tr -d ' ')"`)
	data := make([]byte, maxDocumentSize)
	got, err := Parse(context.Background(), data, "limit.bin")
	if err != nil {
		t.Fatalf("exact maximum rejected: %v", err)
	}
	if got != strconv.Itoa(maxDocumentSize) {
		t.Fatalf("fake lit saw %q bytes, want %d", got, maxDocumentSize)
	}

	over := append(data, 0)
	got, err = Parse(context.Background(), over, "over.bin")
	if err == nil {
		t.Fatal("maximum+1 document accepted")
	}
	if got != "" {
		t.Fatalf("oversized document returned output %q", got)
	}
	want := fmt.Sprintf("document too large (%d bytes, max %d)", maxDocumentSize+1, maxDocumentSize)
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestBoundaryParseFilenameExtensionMatrix(t *testing.T) {
	installBoundaryLit(t, `basename "$2"`)
	tests := []struct {
		name     string
		fileName string
		wantBase string
	}{
		{
			name:     "empty filename",
			fileName: "",
			wantBase: "input",
		},
		{
			name:     "no extension",
			fileName: "README",
			wantBase: "input",
		},
		{
			name:     "pdf lowercase",
			fileName: "report.pdf",
			wantBase: "input.pdf",
		},
		{
			name:     "pdf uppercase",
			fileName: "REPORT.PDF",
			wantBase: "input.PDF",
		},
		{
			name:     "docx",
			fileName: "contract.docx",
			wantBase: "input.docx",
		},
		{
			name:     "xlsx",
			fileName: "budget.xlsx",
			wantBase: "input.xlsx",
		},
		{
			name:     "pptx",
			fileName: "deck.pptx",
			wantBase: "input.pptx",
		},
		{
			name:     "odt",
			fileName: "memo.odt",
			wantBase: "input.odt",
		},
		{
			name:     "ods",
			fileName: "data.ods",
			wantBase: "input.ods",
		},
		{
			name:     "odp",
			fileName: "slides.odp",
			wantBase: "input.odp",
		},
		{
			name:     "png",
			fileName: "scan.png",
			wantBase: "input.png",
		},
		{
			name:     "jpeg",
			fileName: "photo.jpeg",
			wantBase: "input.jpeg",
		},
		{
			name:     "multiple dots use last extension",
			fileName: "archive.contract.final.pdf",
			wantBase: "input.pdf",
		},
		{
			name:     "leading dot is extension",
			fileName: ".pdf",
			wantBase: "input.pdf",
		},
		{
			name:     "trailing dot preserved",
			fileName: "report.",
			wantBase: "input.",
		},
		{
			name:     "path components discarded",
			fileName: "folder/subfolder/report.pdf",
			wantBase: "input.pdf",
		},
		{
			name:     "windows separators cannot escape temp dir",
			fileName: `C:\\Users\\operator\\report.docx`,
			wantBase: "input.docx",
		},
		{
			name:     "parent traversal discarded",
			fileName: "../../outside/secret.pdf",
			wantBase: "input.pdf",
		},
		{
			name:     "spaces in stem discarded",
			fileName: "quarterly report 2026.xlsx",
			wantBase: "input.xlsx",
		},
		{
			name:     "unicode stem discarded",
			fileName: "결산 보고서.pptx",
			wantBase: "input.pptx",
		},
		{
			name:     "unicode extension retained",
			fileName: "report.문서",
			wantBase: "input.문서",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(context.Background(), []byte("document"), tt.fileName)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got != tt.wantBase {
				t.Fatalf("temporary basename = %q, want %q", got, tt.wantBase)
			}
			if strings.Contains(got, "..") || strings.ContainsAny(got, `/\\`) {
				t.Fatalf("unsafe temporary basename: %q", got)
			}
		})
	}
}

func TestBoundaryParseCommandArgumentContract(t *testing.T) {
	installBoundaryLit(t, `printf 'arg1=%s\narg2=%s\nargc=%s\n' "$1" "$2" "$#"`)
	got, err := Parse(context.Background(), []byte("document"), "report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("argument output = %q", got)
	}
	if lines[0] != "arg1=parse" {
		t.Fatalf("first argument = %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "/input.pdf") {
		t.Fatalf("second argument did not point to preserved extension: %q", lines[1])
	}
	if lines[2] != "argc=2" {
		t.Fatalf("argument count = %q", lines[2])
	}
}

func TestBoundaryParseTemporaryInputPermissions(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("stat flags are Linux-specific")
	}
	installBoundaryLit(t, `stat -c '%a' "$2"`)
	got, err := Parse(context.Background(), []byte("sensitive"), "report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if got != "600" {
		t.Fatalf("temporary input permissions = %q, want 600", got)
	}
}

func TestBoundaryParseRoundTripsBinaryInput(t *testing.T) {
	installBoundaryLit(t, `od -An -tx1 -v "$2" | tr -d ' \n'`)
	data := []byte{0x00, 0x01, 0x7f, 0x80, 0xfe, 0xff, '\n', '\r'}
	got, err := Parse(context.Background(), data, "blob.bin")
	if err != nil {
		t.Fatal(err)
	}
	if got != "00017f80feff0a0d" {
		t.Fatalf("binary round trip = %q", got)
	}
}

func TestBoundaryParseSuccessOutputWhitespaceMatrix(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{
			name:   "empty stdout",
			script: `:`,
			want:   "",
		},
		{
			name:   "spaces only",
			script: `printf '   '`,
			want:   "",
		},
		{
			name:   "newlines only",
			script: `printf '\n\n'`,
			want:   "",
		},
		{
			name:   "leading spaces trimmed",
			script: `printf '   text'`,
			want:   "text",
		},
		{
			name:   "trailing spaces trimmed",
			script: `printf 'text   '`,
			want:   "text",
		},
		{
			name:   "leading newline trimmed",
			script: `printf '\ntext'`,
			want:   "text",
		},
		{
			name:   "trailing newline trimmed",
			script: `printf 'text\n'`,
			want:   "text",
		},
		{
			name:   "internal spaces retained",
			script: `printf 'first   second'`,
			want:   "first   second",
		},
		{
			name:   "internal blank lines retained",
			script: `printf 'first\n\nsecond'`,
			want:   "first\n\nsecond",
		},
		{
			name:   "unicode retained",
			script: `printf '  결산 📎 완료  '`,
			want:   "결산 📎 완료",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installBoundaryLit(t, tt.script)
			got, err := Parse(context.Background(), []byte("document"), "file.txt")
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got != tt.want {
				t.Fatalf("output = %q, want %q", got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("output is invalid UTF-8: %x", []byte(got))
			}
		})
	}
}

func TestBoundaryParseFailureErrorSelectionMatrix(t *testing.T) {
	tests := []struct {
		name      string
		script    string
		wantError string
	}{
		{
			name:      "stderr becomes detail",
			script:    `printf 'parser rejected input' >&2; exit 7`,
			wantError: "lit parse failed: parser rejected input",
		},
		{
			name:      "stderr surrounding whitespace trimmed",
			script:    `printf '  malformed office file  \n' >&2; exit 8`,
			wantError: "lit parse failed: malformed office file",
		},
		{
			name:      "multiline stderr retained",
			script:    `printf 'first line\nsecond line\n' >&2; exit 9`,
			wantError: "lit parse failed: first line\nsecond line",
		},
		{
			name:      "empty stderr falls back to exit status",
			script:    `exit 11`,
			wantError: "lit parse failed: exit status 11",
		},
		{
			name:      "whitespace stderr falls back to exit status",
			script:    `printf ' \n\t ' >&2; exit 12`,
			wantError: "lit parse failed: exit status 12",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installBoundaryLit(t, tt.script)
			got, err := Parse(context.Background(), []byte("document"), "file.pdf")
			if err == nil {
				t.Fatalf("Parse succeeded with output %q", got)
			}
			if got != "" {
				t.Fatalf("failure returned output %q", got)
			}
			if err.Error() != tt.wantError {
				t.Fatalf("error = %q, want %q", err, tt.wantError)
			}
		})
	}
}

func TestBoundaryParseFailureReturnsTrimmedPartialOutput(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{
			name:   "single partial line",
			script: `printf 'partial'; exit 1`,
			want:   "partial",
		},
		{
			name:   "partial beats stderr",
			script: `printf 'usable'; printf 'fatal' >&2; exit 2`,
			want:   "usable",
		},
		{
			name:   "partial surrounding whitespace trimmed",
			script: `printf '  usable partial  \n'; exit 3`,
			want:   "usable partial",
		},
		{
			name:   "partial preserves internal newlines",
			script: `printf 'first\n\nsecond'; exit 4`,
			want:   "first\n\nsecond",
		},
		{
			name:   "partial unicode",
			script: `printf '  일부 📄 결과  '; exit 5`,
			want:   "일부 📄 결과",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installBoundaryLit(t, tt.script)
			got, err := Parse(context.Background(), []byte("document"), "file.pdf")
			if err != nil {
				t.Fatalf("partial output should suppress command error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("partial output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBoundaryParseSuccessfulOutputTruncation(t *testing.T) {
	installBoundaryLit(t, fmt.Sprintf(`head -c %d /dev/zero | tr '\000' 'x'`, MaxOutputBytes+73))
	got, err := Parse(context.Background(), []byte("document"), "file.pdf")
	if err != nil {
		t.Fatal(err)
	}
	suffix := "\n\n[... 텍스트가 너무 길어 잘렸습니다]"
	if len(got) != MaxOutputBytes+len(suffix) {
		t.Fatalf("truncated length = %d, want %d", len(got), MaxOutputBytes+len(suffix))
	}
	if !strings.HasPrefix(got, strings.Repeat("x", 128)) {
		t.Fatal("truncated output lost prefix")
	}
	if !strings.HasSuffix(got, suffix) {
		t.Fatalf("truncated output missing marker: %q", got[len(got)-80:])
	}
}

func TestBoundaryParseExactMaximumOutputIsNotTruncated(t *testing.T) {
	installBoundaryLit(t, fmt.Sprintf(`head -c %d /dev/zero | tr '\000' 'y'`, MaxOutputBytes))
	got, err := Parse(context.Background(), []byte("document"), "file.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != MaxOutputBytes {
		t.Fatalf("exact maximum output length = %d", len(got))
	}
	if strings.Contains(got, "텍스트가 너무 길어") {
		t.Fatal("exact maximum output was unnecessarily truncated")
	}
}

func TestBoundaryParsePartialOutputTruncation(t *testing.T) {
	installBoundaryLit(t, fmt.Sprintf(`head -c %d /dev/zero | tr '\000' 'z'; exit 17`, MaxOutputBytes+91))
	got, err := Parse(context.Background(), []byte("document"), "file.pdf")
	if err != nil {
		t.Fatalf("partial output should remain usable: %v", err)
	}
	suffix := "\n\n[... 텍스트가 너무 길어 잘렸습니다]"
	if len(got) != MaxOutputBytes+len(suffix) {
		t.Fatalf("partial truncated length = %d", len(got))
	}
	if !strings.HasSuffix(got, suffix) {
		t.Fatal("partial output missing truncation marker")
	}
}

func TestBoundaryParseContextAlreadyCanceled(t *testing.T) {
	installBoundaryLit(t, `printf 'must-not-complete'; sleep 5`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	got, err := Parse(ctx, []byte("document"), "file.pdf")
	if err == nil {
		t.Fatalf("canceled parse succeeded: %q", got)
	}
	if got != "" {
		t.Fatalf("canceled parse returned output %q", got)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("already-canceled parse took too long: %s", time.Since(started))
	}
	if !strings.Contains(err.Error(), "lit parse failed") {
		t.Fatalf("canceled error lost parser boundary: %v", err)
	}
}

func TestBoundaryParseContextCancellationKillsCommand(t *testing.T) {
	installBoundaryLit(t, `exec sleep 10`)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	got, err := Parse(ctx, []byte("document"), "file.pdf")
	if err == nil {
		t.Fatalf("timed out parse succeeded: %q", got)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("command was not killed promptly: %s", time.Since(started))
	}
	if got != "" {
		t.Fatalf("timed out parse returned output %q", got)
	}
}

func TestBoundaryParsePartialOutputWinsOnCancellation(t *testing.T) {
	installBoundaryLit(t, `printf 'early extraction'; exec sleep 10`)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	got, err := Parse(ctx, []byte("document"), "file.pdf")
	if err != nil {
		t.Fatalf("usable partial output should win on cancellation: %v", err)
	}
	if got != "early extraction" {
		t.Fatalf("partial output = %q", got)
	}
}

func TestBoundaryParseConcurrentDocumentsStayIsolated(t *testing.T) {
	installBoundaryLit(t, `cat "$2"`)
	const workers = 48
	start := make(chan struct{})
	type result struct {
		index int
		text  string
		err   error
	}
	results := make(chan result, workers)
	for i := 0; i < workers; i++ {
		go func() {
			<-start
			want := fmt.Sprintf("document-%03d-%s", i, strings.Repeat("x", i%11))
			got, err := Parse(context.Background(), []byte(want), fmt.Sprintf("input-%d.pdf", i))
			results <- result{index: i, text: got, err: err}
		}()
	}
	close(start)

	seen := make(map[int]bool, workers)
	for i := 0; i < workers; i++ {
		res := <-results
		if res.err != nil {
			t.Fatalf("worker %d: %v", res.index, res.err)
		}
		want := fmt.Sprintf("document-%03d-%s", res.index, strings.Repeat("x", res.index%11))
		if res.text != want {
			t.Fatalf("worker %d got %q, want %q", res.index, res.text, want)
		}
		if seen[res.index] {
			t.Fatalf("duplicate result for worker %d", res.index)
		}
		seen[res.index] = true
	}
}

func TestBoundaryParseTemporaryDirectoryRemovedAfterSuccess(t *testing.T) {
	recordDir := t.TempDir()
	recordFile := filepath.Join(recordDir, "input-path")
	installBoundaryLit(t, fmt.Sprintf(`printf '%%s' "$2" > %q; printf ok`, recordFile))
	got, err := Parse(context.Background(), []byte("document"), "file.pdf")
	if err != nil || got != "ok" {
		t.Fatalf("Parse() = %q, %v", got, err)
	}
	raw, err := os.ReadFile(recordFile)
	if err != nil {
		t.Fatal(err)
	}
	inputPath := string(raw)
	if _, err := os.Stat(inputPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary input still exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(inputPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary directory still exists or stat failed unexpectedly: %v", err)
	}
}

func TestBoundaryParseTemporaryDirectoryRemovedAfterFailure(t *testing.T) {
	recordDir := t.TempDir()
	recordFile := filepath.Join(recordDir, "input-path")
	installBoundaryLit(t, fmt.Sprintf(`printf '%%s' "$2" > %q; printf failed >&2; exit 23`, recordFile))
	_, err := Parse(context.Background(), []byte("document"), "file.pdf")
	if err == nil {
		t.Fatal("Parse unexpectedly succeeded")
	}
	raw, readErr := os.ReadFile(recordFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	inputPath := string(raw)
	if _, statErr := os.Stat(inputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("temporary input still exists or stat failed unexpectedly: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Dir(inputPath)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("temporary directory still exists or stat failed unexpectedly: %v", statErr)
	}
}

func TestBoundaryConstantsRemainProtective(t *testing.T) {
	if MaxOutputBytes != 200*1024 {
		t.Fatalf("MaxOutputBytes = %d", MaxOutputBytes)
	}
	if maxDocumentSize != 50*1024*1024 {
		t.Fatalf("maxDocumentSize = %d", maxDocumentSize)
	}
	if parseTimeout != 60*time.Second {
		t.Fatalf("parseTimeout = %s", parseTimeout)
	}
	if MaxOutputBytes >= maxDocumentSize {
		t.Fatalf("output cap %d must remain below input cap %d", MaxOutputBytes, maxDocumentSize)
	}
}
