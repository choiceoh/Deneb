package briefcase

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
)

func TestExportRunArtifactsCreatesExactDetachedBundle(t *testing.T) {
	pack := writeHarnessCase(t)
	pack.Manifest.Artifacts = []casepack.Artifact{
		{ID: "missing", Path: "output/missing.txt", MIME: "text/plain", Required: true, MaxBytes: 64},
		{ID: "binary", Path: "output/nested/data.bin", MIME: "application/octet-stream", Required: true, MaxBytes: 64},
		{ID: "report", Path: "output/report.txt", MIME: "text/plain", Required: true, MaxBytes: 64},
	}
	sourceRoot := t.TempDir()
	binaryPayload := []byte{0x00, 0xff, 0x01, '\n', 'D', 'E', 'N', 'E', 'B'}
	writeExportArtifact(t, sourceRoot, "output/nested/data.bin", binaryPayload)
	writeExportArtifact(t, sourceRoot, "output/report.txt", []byte("durable report"))
	writeExportArtifact(t, sourceRoot, "undeclared.txt", []byte("private"))
	run := &RunResult{
		ArtifactRoot: sourceRoot,
		Episodes:     []EpisodeResult{{EpisodeID: "episode-1", Text: "original"}},
	}
	destination := filepath.Join(t.TempDir(), "export")

	exported, err := ExportRunArtifacts(context.Background(), pack, run, destination)
	if err != nil {
		t.Fatal(err)
	}
	if exported == run || run.ArtifactRoot != sourceRoot || exported.ArtifactRoot != destination {
		t.Fatalf("result roots original=%q exported=%q", run.ArtifactRoot, exported.ArtifactRoot)
	}
	exported.Episodes[0].Text = "mutated"
	if run.Episodes[0].Text != "original" {
		t.Fatalf("export result aliases original episodes: %+v", run.Episodes)
	}
	assertExportArtifactBytes(t, destination, "output/nested/data.bin", binaryPayload)
	assertExportArtifactBytes(t, destination, "output/report.txt", []byte("durable report"))
	for _, relative := range []string{"output/missing.txt", "undeclared.txt"} {
		if _, err := os.Stat(filepath.Join(destination, filepath.FromSlash(relative))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected exported path %q: %v", relative, err)
		}
	}
	info, err := os.Stat(filepath.Join(destination, "output", "report.txt"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("exported mode = %v, %v", info, err)
	}
	for _, directory := range []string{destination, filepath.Join(destination, "output", "nested")} {
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("export directory %q mode = %v, %v", directory, info, err)
		}
	}
	if _, err := ExportRunArtifacts(context.Background(), pack, run, destination); err == nil {
		t.Fatal("existing export destination was accepted")
	}
}

func TestExportRunArtifactsRollsBackAtFirstDeclaredFailure(t *testing.T) {
	pack := writeHarnessCase(t)
	pack.Manifest.Artifacts = []casepack.Artifact{
		{ID: "first", Path: "output/first.txt", MIME: "text/plain", Required: true, MaxBytes: 64},
		{ID: "second", Path: "output/second.txt", MIME: "text/plain", Required: true, MaxBytes: 64},
		{ID: "third", Path: "output/third.txt", MIME: "text/plain", Required: true, MaxBytes: 1},
	}
	sourceRoot := t.TempDir()
	writeExportArtifact(t, sourceRoot, "output/first.txt", []byte("first"))
	if err := os.MkdirAll(filepath.Join(sourceRoot, "output", "second.txt"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeExportArtifact(t, sourceRoot, "output/third.txt", []byte("too large"))
	destination := filepath.Join(t.TempDir(), "export")

	exported, err := ExportRunArtifacts(context.Background(), pack, &RunResult{ArtifactRoot: sourceRoot}, destination)
	if exported != nil || err == nil || !strings.Contains(err.Error(), `artifact "second" is not a regular file`) {
		t.Fatalf("ordered failure result=%+v error=%v", exported, err)
	}
	if strings.Contains(err.Error(), "third") {
		t.Fatalf("later artifact replaced first declared failure: %v", err)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial export destination survived: %v", statErr)
	}
}

func TestExportRunArtifactsCancellationRemovesUncommittedBundle(t *testing.T) {
	pack := writeHarnessCase(t)
	pack.Manifest.Artifacts = []casepack.Artifact{{
		ID: "report", Path: "output/report.txt", MIME: "text/plain", Required: true, MaxBytes: 64,
	}}
	sourceRoot := t.TempDir()
	writeExportArtifact(t, sourceRoot, "output/report.txt", []byte("report"))
	destination := filepath.Join(t.TempDir(), "export")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	exported, err := ExportRunArtifacts(ctx, pack, &RunResult{ArtifactRoot: sourceRoot}, destination)
	if exported != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled export result=%+v error=%v", exported, err)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("canceled export destination survived: %v", statErr)
	}
}

func TestExportRunArtifactsCancellationAtCommitBoundaryRollsBack(t *testing.T) {
	pack := writeHarnessCase(t)
	pack.Manifest.Artifacts = []casepack.Artifact{{
		ID: "empty", Path: "output/empty.txt", MIME: "text/plain", Required: true, MaxBytes: 64,
	}}
	sourceRoot := t.TempDir()
	writeExportArtifact(t, sourceRoot, "output/empty.txt", nil)
	destination := filepath.Join(t.TempDir(), "export")
	ctx := &cancelOnErrCallContext{Context: context.Background(), cancelOn: 4}

	exported, err := ExportRunArtifacts(ctx, pack, &RunResult{ArtifactRoot: sourceRoot}, destination)
	if exported != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("commit-boundary cancellation result=%+v error=%v", exported, err)
	}
	if ctx.calls != ctx.cancelOn {
		t.Fatalf("context checks = %d, want cancellation at check %d", ctx.calls, ctx.cancelOn)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("commit-boundary cancellation left destination: %v", statErr)
	}
}

func TestSerializeArtifactPayloadPreservesBytesAndFailures(t *testing.T) {
	payload := bytes.Repeat([]byte{0x00, 0xff, 'A', '\n'}, 20_000)
	var exact bytes.Buffer
	if err := serializeArtifactPayload(context.Background(), bytes.NewReader(payload), &exact); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(exact.Bytes(), payload) {
		t.Fatalf("serialized payload length = %d, want exact %d bytes", exact.Len(), len(payload))
	}

	writeErr := errors.New("target failed")
	failing := &errorAfterWriter{remaining: 17, err: writeErr}
	if err := serializeArtifactPayload(context.Background(), bytes.NewReader(payload), failing); !errors.Is(err, writeErr) {
		t.Fatalf("partial write error = %v, want %v", err, writeErr)
	}
	if !bytes.Equal(failing.Bytes(), payload[:17]) {
		t.Fatalf("partial bytes = %x, want %x", failing.Bytes(), payload[:17])
	}
	if err := serializeArtifactPayload(context.Background(), strings.NewReader("short"), shortArtifactWriter{}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write error = %v, want io.ErrShortWrite", err)
	}
}

func TestExportRunArtifactsRejectsDestinationAliases(t *testing.T) {
	pack := writeHarnessCase(t)
	pack.Manifest.Artifacts = []casepack.Artifact{{
		ID: "report", Path: "output/report.txt", MIME: "text/plain", Required: true, MaxBytes: 64,
	}}
	sourceRoot := t.TempDir()
	writeExportArtifact(t, sourceRoot, "output/report.txt", []byte("report"))
	link := filepath.Join(t.TempDir(), "source-link")
	if err := os.Symlink(sourceRoot, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := ExportRunArtifacts(context.Background(), pack, &RunResult{ArtifactRoot: sourceRoot}, filepath.Join(link, "escaped")); err == nil || !strings.Contains(err.Error(), "outside the run root") {
		t.Fatalf("symlink-parent export error = %v", err)
	}
}

func TestExportRunArtifactsRejectsDurableDestinationInsidePlaintextRunRoot(t *testing.T) {
	pack := writeHarnessCase(t)
	pack.Manifest.Artifacts = []casepack.Artifact{{
		ID: "report", Path: "output/report.txt", MIME: "text/plain", Required: true, MaxBytes: 64,
	}}
	runRoot, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runRoot.Close()
	paths, err := runRoot.Paths()
	if err != nil {
		t.Fatal(err)
	}
	writeExportArtifact(t, paths.Workspace, "output/report.txt", []byte("report"))
	link := filepath.Join(t.TempDir(), "run-link")
	if err := os.Symlink(paths.Root, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err = ExportRunArtifacts(context.Background(), pack, &RunResult{ArtifactRoot: paths.Workspace}, filepath.Join(link, "durable"))
	if err == nil || !strings.Contains(err.Error(), "plaintext RunRoot") {
		t.Fatalf("inside-RunRoot durable export error = %v", err)
	}
}

type errorAfterWriter struct {
	bytes.Buffer
	remaining int
	err       error
}

func (w *errorAfterWriter) Write(payload []byte) (int, error) {
	if w.remaining <= 0 {
		return 0, w.err
	}
	n := min(w.remaining, len(payload))
	_, _ = w.Buffer.Write(payload[:n])
	w.remaining -= n
	if n < len(payload) {
		return n, w.err
	}
	return n, nil
}

type shortArtifactWriter struct{}

func (shortArtifactWriter) Write(payload []byte) (int, error) {
	return len(payload) - 1, nil
}

type cancelOnErrCallContext struct {
	context.Context
	cancelOn int
	calls    int
}

func (ctx *cancelOnErrCallContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelOn {
		return context.Canceled
	}
	return nil
}

func writeExportArtifact(t *testing.T, root, relative string, payload []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertExportArtifactBytes(t *testing.T, root, relative string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("artifact %q = %x, want %x", relative, got, want)
	}
}
