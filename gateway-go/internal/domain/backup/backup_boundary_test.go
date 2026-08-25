package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type archivedEntry struct {
	header  tar.Header
	content []byte
}

func readArchiveEntries(t *testing.T, data []byte) map[string]archivedEntry {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	entries := make(map[string]archivedEntry)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %q: %v", hdr.Name, err)
		}
		entries[hdr.Name] = archivedEntry{header: *hdr, content: body}
	}
	return entries
}

func makeArchive(t *testing.T, stateDir string, targets []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := writeArchive(&buf, stateDir, targets); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}
	return buf.Bytes()
}

func writeBoundaryFile(t *testing.T, root, rel, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	return path
}

func TestNewTaskRejectsBlankRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
		part string
	}{
		{
			name: "empty state directory",
			cfg:  Config{StateDir: "", SSHHost: "storage"},
			part: "StateDir required",
		},
		{
			name: "space state directory",
			cfg:  Config{StateDir: "   ", SSHHost: "storage"},
			part: "StateDir required",
		},
		{
			name: "tab state directory",
			cfg:  Config{StateDir: "\t\n", SSHHost: "storage"},
			part: "StateDir required",
		},
		{
			name: "empty ssh host",
			cfg:  Config{StateDir: "/state", SSHHost: ""},
			part: "SSHHost required",
		},
		{
			name: "space ssh host",
			cfg:  Config{StateDir: "/state", SSHHost: "  "},
			part: "SSHHost required",
		},
		{
			name: "newline ssh host",
			cfg:  Config{StateDir: "/state", SSHHost: "\n"},
			part: "SSHHost required",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewTask(tc.cfg, nil)
			if got != nil {
				t.Fatalf("NewTask returned task %#v with invalid config", got)
			}
			if err == nil || !strings.Contains(err.Error(), tc.part) {
				t.Fatalf("error = %v, want containing %q", err, tc.part)
			}
		})
	}
}

func TestNewTaskRejectsRemoteDirectoryShellSyntax(t *testing.T) {
	t.Parallel()

	bad := []struct {
		name string
		dir  string
	}{
		{name: "space", dir: "backup files"},
		{name: "single quote", dir: "backup'files"},
		{name: "double quote", dir: `backup"files`},
		{name: "backtick", dir: "backup`id`"},
		{name: "dollar", dir: "backup$HOME"},
		{name: "semicolon", dir: "backup;rm"},
		{name: "pipe", dir: "backup|tee"},
		{name: "ampersand", dir: "backup&sleep"},
		{name: "redirect output", dir: "backup>file"},
		{name: "redirect input", dir: "backup<file"},
		{name: "newline is whitespace", dir: "backup\nfiles"},
		{name: "tab is whitespace", dir: "backup\tfiles"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			task, err := NewTask(Config{
				StateDir:  "/state",
				SSHHost:   "storage",
				RemoteDir: tc.dir,
			}, nil)
			if task != nil {
				t.Fatalf("NewTask accepted RemoteDir %q", tc.dir)
			}
			if err == nil || !strings.Contains(err.Error(), "RemoteDir") {
				t.Fatalf("error = %v, want RemoteDir validation", err)
			}
		})
	}
}

func TestNewTaskRejectsSSHHostOptionAndShellSyntax(t *testing.T) {
	t.Parallel()

	bad := []struct {
		name string
		host string
	}{
		{name: "leading option", host: "-oProxyCommand=evil"},
		{name: "leading dash only", host: "-"},
		{name: "space", host: "storage host"},
		{name: "single quote", host: "storage'host"},
		{name: "double quote", host: `storage"host`},
		{name: "backtick", host: "storage`id`"},
		{name: "dollar", host: "storage$HOME"},
		{name: "semicolon", host: "storage;rm"},
		{name: "pipe", host: "storage|tee"},
		{name: "ampersand", host: "storage&sleep"},
		{name: "redirect output", host: "storage>file"},
		{name: "redirect input", host: "storage<file"},
		{name: "newline", host: "storage\nhost"},
		{name: "tab", host: "storage\thost"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			task, err := NewTask(Config{StateDir: "/state", SSHHost: tc.host}, nil)
			if task != nil {
				t.Fatalf("NewTask accepted SSHHost %q", tc.host)
			}
			if err == nil || !strings.Contains(err.Error(), "invalid SSHHost") {
				t.Fatalf("error = %v, want invalid SSHHost validation", err)
			}
		})
	}
}

func TestNewTaskPreservesSafeHostAndDirectoryShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		dir  string
	}{
		{name: "simple", host: "storage", dir: "archives"},
		{name: "dns", host: "storage.example.com", dir: "deneb-backups"},
		{name: "ipv4", host: "192.0.2.17", dir: "backups/primary"},
		{name: "user at host", host: "backup@storage", dir: "archive_2026"},
		{name: "host alias underscore", host: "storage_backup", dir: "a.b-c_d"},
		{name: "ipv6-ish alias", host: "[2001:db8::1]", dir: "snapshots/daily"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			task, err := NewTask(Config{StateDir: "/state", SSHHost: tc.host, RemoteDir: tc.dir}, nil)
			if err != nil {
				t.Fatalf("NewTask: %v", err)
			}
			if task.cfg.SSHHost != tc.host || task.cfg.RemoteDir != tc.dir {
				t.Fatalf("config changed: %#v", task.cfg)
			}
			if task.ship == nil || task.prune == nil {
				t.Fatalf("transport callbacks not initialized: %#v", task)
			}
		})
	}
}

func TestNewTaskDefaultsAndPeriodicContract(t *testing.T) {
	t.Parallel()

	pre := func(context.Context) {}
	task, err := NewTask(Config{StateDir: "/state", SSHHost: "storage"}, pre)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	if task.cfg.RemoteDir != "deneb-backups" {
		t.Fatalf("RemoteDir = %q", task.cfg.RemoteDir)
	}
	if task.cfg.RetentionDays != 30 {
		t.Fatalf("RetentionDays = %d", task.cfg.RetentionDays)
	}
	if task.cfg.Logger == nil {
		t.Fatal("Logger is nil")
	}
	if task.preSnapshot == nil {
		t.Fatal("preSnapshot was dropped")
	}
	if task.Name() != "memory-backup" {
		t.Fatalf("Name = %q", task.Name())
	}
	// Hourly RETRY cadence for one daily archive — Run gates on the remote
	// existence check, so a failed attempt retries within the hour instead of
	// losing the day (see Interval's doc comment).
	if task.Interval() != time.Hour {
		t.Fatalf("Interval = %v", task.Interval())
	}
	if task.shipped == nil {
		t.Fatal("shipped probe was not wired — Run would re-ship every hour")
	}
}

func TestNewTaskRetentionBoundaryUsesDefaultForNonPositive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "large negative", in: -1_000_000, want: 30},
		{name: "negative one", in: -1, want: 30},
		{name: "zero", in: 0, want: 30},
		{name: "one", in: 1, want: 1},
		{name: "thirty", in: 30, want: 30},
		{name: "large positive", in: 3650, want: 3650},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			task, err := NewTask(Config{
				StateDir:      "/state",
				SSHHost:       "storage",
				RetentionDays: tc.in,
			}, nil)
			if err != nil {
				t.Fatalf("NewTask: %v", err)
			}
			if task.cfg.RetentionDays != tc.want {
				t.Fatalf("RetentionDays = %d, want %d", task.cfg.RetentionDays, tc.want)
			}
		})
	}
}

func TestWriteArchivePreservesFileContentMetadataAndHierarchy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := []struct {
		rel     string
		body    string
		mode    os.FileMode
		modTime time.Time
	}{
		{
			rel:     "wiki/index.md",
			body:    "# Index\n한글 본문\n",
			mode:    0o640,
			modTime: time.Unix(1_700_000_000, 0),
		},
		{
			rel:     "wiki/projects/deep/path.md",
			body:    strings.Repeat("payload-", 512),
			mode:    0o600,
			modTime: time.Unix(1_710_000_000, 0),
		},
		{
			rel:     "memory/2026-07-11.md",
			body:    "오늘의 기억\n두 번째 줄\n",
			mode:    0o644,
			modTime: time.Unix(1_720_000_000, 0),
		},
		{
			rel:     "contacts.json",
			body:    `{"contacts":[{"id":"fake"}]}`,
			mode:    0o600,
			modTime: time.Unix(1_730_000_000, 0),
		},
		{
			rel:     "kv.json",
			body:    "{}\n",
			mode:    0o664,
			modTime: time.Unix(1_740_000_000, 0),
		},
	}
	actualModes := make(map[string]os.FileMode, len(files))
	for _, file := range files {
		path := writeBoundaryFile(t, dir, file.rel, file.body, file.mode)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		actualModes[file.rel] = info.Mode().Perm()
		if err := os.Chtimes(path, file.modTime, file.modTime); err != nil {
			t.Fatalf("Chtimes(%s): %v", path, err)
		}
	}
	entries := readArchiveEntries(t, makeArchive(t, dir, []string{"wiki", "memory", "contacts.json", "kv.json"}))
	for _, file := range files {
		entry, ok := entries[file.rel]
		if !ok {
			t.Errorf("archive missing %q; entries=%v", file.rel, sortedArchiveNames(entries))
			continue
		}
		if got := string(entry.content); got != file.body {
			t.Errorf("%s content = %q, want %q", file.rel, got, file.body)
		}
		if got := os.FileMode(entry.header.Mode).Perm(); got != actualModes[file.rel] {
			t.Errorf("%s mode = %o, want source mode %o", file.rel, got, actualModes[file.rel])
		}
		if !entry.header.ModTime.Equal(file.modTime) {
			t.Errorf("%s ModTime = %v, want %v", file.rel, entry.header.ModTime, file.modTime)
		}
		if entry.header.Typeflag != tar.TypeReg {
			t.Errorf("%s type = %d, want regular", file.rel, entry.header.Typeflag)
		}
	}
	for _, dirName := range []string{"wiki/", "wiki/projects/", "wiki/projects/deep/", "memory/"} {
		entry, ok := entries[dirName]
		if !ok {
			t.Errorf("archive missing directory header %q", dirName)
			continue
		}
		if entry.header.Typeflag != tar.TypeDir {
			t.Errorf("%s type = %d, want directory", dirName, entry.header.Typeflag)
		}
		if got := os.FileMode(entry.header.Mode).Perm(); got != 0o755 {
			t.Errorf("%s archived mode = %o, want normalized 755", dirName, got)
		}
	}
}

func TestWriteArchiveExcludesTransientSuffixesAtEveryDepth(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := []struct {
		rel  string
		keep bool
	}{
		{rel: "wiki/keep.md", keep: true},
		{rel: "wiki/top.tmp", keep: false},
		{rel: "wiki/top.lock", keep: false},
		{rel: "wiki/top.partial", keep: false},
		{rel: "wiki/nested/deep.tmp", keep: false},
		{rel: "wiki/nested/deep.lock", keep: false},
		{rel: "wiki/nested/deep.partial", keep: false},
		{rel: "wiki/name.tmp.md", keep: true},
		{rel: "wiki/name.lock.md", keep: true},
		{rel: "wiki/name.partial.md", keep: true},
		{rel: "wiki/UPPER.TMP", keep: true},
		{rel: "wiki/UPPER.LOCK", keep: true},
		{rel: "wiki/UPPER.PARTIAL", keep: true},
		{rel: "wiki/dottmp", keep: true},
		{rel: "wiki/tmp", keep: true},
	}
	for _, file := range files {
		writeBoundaryFile(t, dir, file.rel, file.rel, 0o600)
	}
	entries := readArchiveEntries(t, makeArchive(t, dir, []string{"wiki"}))
	for _, file := range files {
		_, found := entries[file.rel]
		if found != file.keep {
			t.Errorf("entry %q found=%v, want %v", file.rel, found, file.keep)
		}
	}
}

func TestWriteArchiveSkipsMissingTargetsWithoutPlaceholder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeBoundaryFile(t, dir, "wiki/present.md", "present", 0o600)
	targets := []string{
		"missing-file.json",
		"wiki",
		"missing-directory",
		"another/missing/path",
	}
	entries := readArchiveEntries(t, makeArchive(t, dir, targets))
	if _, ok := entries["wiki/present.md"]; !ok {
		t.Fatalf("present target omitted: %v", sortedArchiveNames(entries))
	}
	for name := range entries {
		if strings.Contains(name, "missing") {
			t.Errorf("missing target produced placeholder %q", name)
		}
	}
}

func TestWriteArchiveSkipsSymlinksAndOtherNonRegularEntries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior requires Unix-like permissions")
	}
	t.Parallel()

	dir := t.TempDir()
	outside := writeBoundaryFile(t, t.TempDir(), "outside-secret", "must not follow", 0o600)
	writeBoundaryFile(t, dir, "wiki/regular.md", "regular", 0o600)
	if err := os.Symlink(outside, filepath.Join(dir, "wiki", "file-link")); err != nil {
		t.Fatalf("Symlink file: %v", err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(dir, "wiki", "dir-link")); err != nil {
		t.Fatalf("Symlink dir: %v", err)
	}
	entries := readArchiveEntries(t, makeArchive(t, dir, []string{"wiki", "wiki/file-link", "wiki/dir-link"}))
	if _, ok := entries["wiki/regular.md"]; !ok {
		t.Fatal("regular file missing")
	}
	for _, name := range []string{"wiki/file-link", "wiki/dir-link", "outside-secret"} {
		if _, ok := entries[name]; ok {
			t.Errorf("non-regular entry %q was archived", name)
		}
	}
}

func TestWriteArchiveSupportsUnicodeAndPunctuationPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := map[string]string{
		"wiki/프로젝트/태양광 발전.md":             "한글 파일",
		"wiki/emoji/launch-🚀.md":          "emoji path",
		"wiki/punctuation/a,b;c(1)[2].md": "punctuation is safe inside tar",
		"memory/2026-07-11 서울.md":         "daily",
		"workspace/AGENTS.default.md":     "agent config",
	}
	for rel, body := range files {
		writeBoundaryFile(t, dir, rel, body, 0o600)
	}
	entries := readArchiveEntries(t, makeArchive(t, dir, []string{"wiki", "memory", "workspace"}))
	for rel, body := range files {
		entry, ok := entries[rel]
		if !ok {
			t.Errorf("unicode entry %q missing; have %v", rel, sortedArchiveNames(entries))
			continue
		}
		if string(entry.content) != body {
			t.Errorf("%q body = %q, want %q", rel, entry.content, body)
		}
	}
}

func TestWriteArchiveTargetOrderControlsTarOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeBoundaryFile(t, dir, "wiki/a.md", "a", 0o600)
	writeBoundaryFile(t, dir, "memory/b.md", "b", 0o600)
	data := makeArchive(t, dir, []string{"memory", "wiki"})
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
	}
	want := []string{"memory/", "memory/b.md", "wiki/", "wiki/a.md"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("tar order = %v, want %v", names, want)
	}
}

func TestWriteArchiveEmptyTargetListProducesValidEmptyArchive(t *testing.T) {
	t.Parallel()

	data := makeArchive(t, t.TempDir(), nil)
	if len(data) == 0 {
		t.Fatal("writeArchive returned no gzip stream")
	}
	entries := readArchiveEntries(t, data)
	if len(entries) != 0 {
		t.Fatalf("empty targets archived entries: %v", sortedArchiveNames(entries))
	}
}

func TestWriteArchivePropagatesDestinationFailures(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeBoundaryFile(t, dir, "wiki/large.md", strings.Repeat("x", 64*1024), 0o600)
	want := errors.New("destination failed")
	w := &failAfterWriter{remaining: 80, err: want}
	err := writeArchive(w, dir, []string{"wiki"})
	if err == nil {
		t.Fatal("writeArchive returned nil")
	}
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapping %v", err, want)
	}
}

func TestAddFileMissingPathIsBestEffortSkip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeBoundaryFile(t, dir, "gone.md", "body", 0o600)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := addFile(tw, path, "gone.md", info); err != nil {
		t.Fatalf("addFile vanished path = %v, want nil", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(buf.Bytes()))
	if hdr, err := tr.Next(); !errors.Is(err, io.EOF) || hdr != nil {
		t.Fatalf("vanished file wrote header=%#v err=%v", hdr, err)
	}
}

func TestAddFileDetectsTruncationAgainstHeaderSize(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeBoundaryFile(t, dir, "truncated.jsonl", strings.Repeat("record\n", 100), 0o600)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, 3); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err = addFile(tw, path, "transcripts/truncated.jsonl", info)
	if err == nil || !strings.Contains(err.Error(), "truncated while archiving") {
		t.Fatalf("addFile error = %v, want truncation", err)
	}
	if !strings.Contains(err.Error(), "transcripts/truncated.jsonl") {
		t.Fatalf("truncation error lacks archive name: %v", err)
	}
}

func TestAddFilePropagatesTarHeaderFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeBoundaryFile(t, dir, "data", "body", 0o600)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("header sink failed")
	tw := tar.NewWriter(&failAfterWriter{remaining: 0, err: want})
	err = addFile(tw, path, "data", info)
	if !errors.Is(err, want) {
		t.Fatalf("addFile error = %v, want %v", err, want)
	}
}

func TestTaskRunCreatesSnapshotBeforeShipAndPrune(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeBoundaryFile(t, dir, "wiki/page.md", "before", 0o600)
	var mu sync.Mutex
	var calls []string
	task, err := NewTask(Config{StateDir: dir, SSHHost: "storage"}, func(context.Context) {
		mu.Lock()
		calls = append(calls, "snapshot")
		mu.Unlock()
		writeBoundaryFile(t, dir, "memory/from-snapshot.md", "created first", 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	var shippedName string
	var shipped []byte
	task.ship = func(ctx context.Context, name string, archive io.Reader) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		mu.Lock()
		calls = append(calls, "ship")
		mu.Unlock()
		shippedName = name
		var err error
		shipped, err = io.ReadAll(archive)
		return err
	}
	task.prune = func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		mu.Lock()
		calls = append(calls, "prune")
		mu.Unlock()
		return nil
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"snapshot", "ship", "prune"}) {
		t.Fatalf("call order = %v", calls)
	}
	if !strings.HasPrefix(shippedName, "deneb-memory-") || !strings.HasSuffix(shippedName, ".tar.gz") {
		t.Fatalf("archive name = %q", shippedName)
	}
	entries := readArchiveEntries(t, shipped)
	if got := string(entries["memory/from-snapshot.md"].content); got != "created first" {
		t.Fatalf("pre-snapshot content = %q", got)
	}
}

func TestTaskRunWithoutPreSnapshotStillShipsAndPrunes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	task, err := NewTask(Config{StateDir: dir, SSHHost: "storage"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ships atomic.Int64
	var prunes atomic.Int64
	task.ship = func(_ context.Context, _ string, archive io.Reader) error {
		ships.Add(1)
		_, err := io.Copy(io.Discard, archive)
		return err
	}
	task.prune = func(context.Context) error {
		prunes.Add(1)
		return nil
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ships.Load() != 1 || prunes.Load() != 1 {
		t.Fatalf("ships=%d prunes=%d, want 1 each", ships.Load(), prunes.Load())
	}
}

func TestTaskRunShipErrorWinsAndSkipsPrune(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeBoundaryFile(t, dir, "wiki/large.md", strings.Repeat("payload", 10000), 0o600)
	task, err := NewTask(Config{StateDir: dir, SSHHost: "storage"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("remote disk full")
	var prunes atomic.Int64
	task.ship = func(context.Context, string, io.Reader) error { return want }
	task.prune = func(context.Context) error {
		prunes.Add(1)
		return nil
	}
	if err := task.Run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Run error = %v, want %v", err, want)
	}
	if prunes.Load() != 0 {
		t.Fatalf("prune called %d times after ship failure", prunes.Load())
	}
}

func TestTaskRunPartialReadShipErrorDoesNotLeakArchiveGoroutine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeBoundaryFile(t, dir, "wiki/large.md", strings.Repeat("x", 4*1024*1024), 0o600)
	task, err := NewTask(Config{StateDir: dir, SSHHost: "storage"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("connection reset")
	task.ship = func(_ context.Context, _ string, archive io.Reader) error {
		buf := make([]byte, 31)
		_, _ = archive.Read(buf)
		return want
	}
	task.prune = func(context.Context) error {
		t.Fatal("prune called after ship failure")
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- task.Run(context.Background()) }()
	select {
	case err := <-done:
		if !errors.Is(err, want) {
			t.Fatalf("Run error = %v, want %v", err, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run blocked waiting for archive goroutine after ship stopped reading")
	}
}

func TestTaskRunPruneFailureIsNonFatal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeBoundaryFile(t, dir, "contacts.json", "{}", 0o600)
	var logs bytes.Buffer
	task, err := NewTask(Config{
		StateDir: dir,
		SSHHost:  "storage",
		Logger:   slog.New(slog.NewTextHandler(&logs, nil)),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	task.ship = func(_ context.Context, _ string, archive io.Reader) error {
		_, err := io.Copy(io.Discard, archive)
		return err
	}
	want := errors.New("retention service unavailable")
	task.prune = func(context.Context) error { return want }
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run returned non-fatal prune error: %v", err)
	}
	logText := logs.String()
	if !strings.Contains(logText, "retention prune failed") || !strings.Contains(logText, want.Error()) {
		t.Fatalf("warning log = %q", logText)
	}
	if !strings.Contains(logText, "memory backup shipped") {
		t.Fatalf("success log absent after prune warning: %q", logText)
	}
}

func TestTaskRunPassesCanceledContextToHooks(t *testing.T) {
	t.Parallel()

	task, err := NewTask(Config{StateDir: t.TempDir(), SSHHost: "storage"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var sawCanceled atomic.Bool
	task.ship = func(ctx context.Context, _ string, archive io.Reader) error {
		if errors.Is(ctx.Err(), context.Canceled) {
			sawCanceled.Store(true)
		}
		_, _ = io.Copy(io.Discard, archive)
		return ctx.Err()
	}
	task.prune = func(context.Context) error {
		t.Fatal("prune called after canceled ship")
		return nil
	}
	if err := task.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	if !sawCanceled.Load() {
		t.Fatal("ship did not observe canceled context")
	}
}

func TestConcurrentWriteArchiveProducesIndependentValidStreams(t *testing.T) {
	const workers = 24
	dir := t.TempDir()
	for i := 0; i < 40; i++ {
		writeBoundaryFile(t, dir, fmt.Sprintf("wiki/page-%02d.md", i), strings.Repeat(fmt.Sprintf("%02d", i), 200), 0o600)
	}
	start := make(chan struct{})
	results := make(chan []byte, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			var buf bytes.Buffer
			if err := writeArchive(&buf, dir, []string{"wiki"}); err != nil {
				errs <- err
				return
			}
			results <- buf.Bytes()
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Errorf("writeArchive: %v", err)
	}
	count := 0
	for data := range results {
		count++
		entries := readArchiveEntries(t, data)
		for i := 0; i < 40; i++ {
			name := fmt.Sprintf("wiki/page-%02d.md", i)
			if _, ok := entries[name]; !ok {
				t.Errorf("archive %d missing %s", count, name)
			}
		}
	}
	if count != workers {
		t.Fatalf("valid archives = %d, want %d", count, workers)
	}
}

func TestDefaultTargetsSatisfyPathSafetyContract(t *testing.T) {
	t.Parallel()

	want := []string{
		"wiki",
		"knowledge",
		"memory",
		"transcripts",
		"polaris",
		"workspace",
		"network", // infra config snapshots (CRS812 switch export) — device-only state
		"contacts.json",
		"kv.json",
		// Recall gold sets — the retrieval bench's ground truth, hand-curated
		// over months and otherwise held in a single unbacked copy.
		"wiki-qa-gold*.jsonl",
	}
	if !reflect.DeepEqual(DefaultTargets, want) {
		t.Fatalf("DefaultTargets = %v, want %v", DefaultTargets, want)
	}
	seen := make(map[string]bool)
	for _, target := range DefaultTargets {
		if filepath.IsAbs(target) {
			t.Errorf("target %q is absolute", target)
		}
		if target == "." || target == ".." || strings.Contains(target, ".."+string(filepath.Separator)) {
			t.Errorf("target %q can escape state dir", target)
		}
		if seen[target] {
			t.Errorf("duplicate target %q", target)
		}
		seen[target] = true
	}
}

func sortedArchiveNames(entries map[string]archivedEntry) []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type failAfterWriter struct {
	remaining int
	err       error
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if w.remaining <= 0 {
		return 0, w.err
	}
	if len(p) <= w.remaining {
		w.remaining -= len(p)
		return len(p), nil
	}
	n := w.remaining
	w.remaining = 0
	return n, w.err
}

// TestRunSkipsWhenTodaysArchiveAlreadyShipped: the catch-up gate's happy path —
// a tick that finds today's archive on the remote does no work at all (no
// snapshot, no ship, no prune), so the hourly retry cadence costs one probe.
func TestRunSkipsWhenTodaysArchiveAlreadyShipped(t *testing.T) {
	dir := t.TempDir()
	writeBoundaryFile(t, dir, "memory/a.md", "x", 0o600)

	snapshots := 0
	task, err := NewTask(Config{StateDir: dir, SSHHost: "storage"}, func(context.Context) { snapshots++ })
	if err != nil {
		t.Fatal(err)
	}
	var probed string
	task.shipped = func(_ context.Context, name string) (bool, error) { probed = name; return true, nil }
	ships, prunes := 0, 0
	task.ship = func(context.Context, string, io.Reader) error { ships++; return nil }
	task.prune = func(context.Context) error { prunes++; return nil }

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if ships != 0 || prunes != 0 || snapshots != 0 {
		t.Errorf("already-shipped tick did work: ships=%d prunes=%d snapshots=%d", ships, prunes, snapshots)
	}
	if want := "deneb-memory-" + time.Now().Format("20060102") + ".tar.gz"; probed != want {
		t.Errorf("probed %q, want today's archive %q", probed, want)
	}
}

// TestRunRetriesSameDayAfterTransientShipFailure is the 2026-08-06 regression:
// one ssh timeout used to lose that whole day (24h interval + date-stamped
// name). A later tick in the SAME day must ship the SAME archive name.
func TestRunRetriesSameDayAfterTransientShipFailure(t *testing.T) {
	dir := t.TempDir()
	writeBoundaryFile(t, dir, "memory/a.md", "x", 0o600)

	task, err := NewTask(Config{StateDir: dir, SSHHost: "storage"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 8, 6, 8, 40, 0, 0, time.UTC)
	task.now = func() time.Time { return day }

	remote := map[string]bool{}
	task.shipped = func(_ context.Context, name string) (bool, error) { return remote[name], nil }
	task.prune = func(context.Context) error { return nil }

	// Tick 1: the transient outage.
	task.ship = func(context.Context, string, io.Reader) error { return errors.New("connection timed out") }
	if err := task.Run(context.Background()); err == nil {
		t.Fatal("failed ship must surface an error")
	}

	// Tick 2, one hour later, same day: the retry succeeds and the archive
	// carries THAT day's date — the backup is delayed, not lost.
	task.now = func() time.Time { return day.Add(time.Hour) }
	var shippedName string
	task.ship = func(_ context.Context, name string, archive io.Reader) error {
		shippedName = name
		remote[name] = true
		_, err := io.ReadAll(archive)
		return err
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("retry = %v, want success", err)
	}
	if shippedName != "deneb-memory-20260806.tar.gz" {
		t.Errorf("retry shipped %q, want the same day's archive", shippedName)
	}

	// Tick 3: now that it landed, further ticks are no-ops.
	task.ship = func(context.Context, string, io.Reader) error {
		t.Fatal("re-shipped an archive that already landed")
		return nil
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("post-success tick = %v, want nil", err)
	}
}

// TestRunAttemptsWhenProbeFails: an unreachable remote makes the probe error;
// "unknown" must fall through to an attempt, never be read as "already done"
// (that would let a network outage silently suppress backups indefinitely).
func TestRunAttemptsWhenProbeFails(t *testing.T) {
	dir := t.TempDir()
	writeBoundaryFile(t, dir, "memory/a.md", "x", 0o600)

	task, err := NewTask(Config{StateDir: dir, SSHHost: "storage"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	task.shipped = func(context.Context, string) (bool, error) { return false, errors.New("network unreachable") }
	shipped := false
	task.ship = func(_ context.Context, _ string, archive io.Reader) error {
		shipped = true
		_, err := io.ReadAll(archive)
		return err
	}
	task.prune = func(context.Context) error { return nil }

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if !shipped {
		t.Error("probe failure suppressed the backup attempt")
	}
}

// The remote ship line is the last defense between a truncated stream and a
// promoted archive. A graceful sender death gives remote cat a clean EOF and a
// zero exit, so without the gzip -t gate the && chain promotes a torso under
// the final date-stamped name — which the hourly catch-up probe then reports
// as "already shipped" for the rest of the day (observed 2026-08-25).
func TestSSHShipCommandVerifiesIntegrityBeforePromotion(t *testing.T) {
	got := sshShipCommand("/backups", "deneb-memory-20260825.tar.gz")
	want := "mkdir -p /backups && cat > /backups/deneb-memory-20260825.tar.gz.partial" +
		" && gzip -t /backups/deneb-memory-20260825.tar.gz.partial" +
		" && mv /backups/deneb-memory-20260825.tar.gz.partial /backups/deneb-memory-20260825.tar.gz"
	if got != want {
		t.Fatalf("ship command drifted:\n got  %s\n want %s", got, want)
	}
}
