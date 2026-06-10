package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeTestStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("wiki/프로젝트/deneb.md", "# Deneb\n장기 기억 페이지")
	mustWrite("wiki/index.md.tmp", "half-written") // excluded artifact
	mustWrite("transcripts/client:main.jsonl", `{"role":"user"}`+"\n")
	mustWrite("contacts.json", `{"contacts":[]}`)
	// "memory", "polaris", "workspace", "kv.json" intentionally absent.
	return dir
}

func archiveNames(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	names := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		names[hdr.Name] = true
	}
	return names
}

func TestWriteArchive_ContentsAndExclusions(t *testing.T) {
	dir := writeTestStore(t)
	var buf bytes.Buffer
	if err := writeArchive(&buf, dir, DefaultTargets); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}
	names := archiveNames(t, buf.Bytes())
	for _, want := range []string{"wiki/프로젝트/deneb.md", "transcripts/client:main.jsonl", "contacts.json"} {
		if !names[want] {
			t.Errorf("archive missing %q (have %v)", want, names)
		}
	}
	if names["wiki/index.md.tmp"] {
		t.Error(".tmp artifacts must be excluded from the archive")
	}
	for n := range names {
		if n == "memory/" || n == "polaris/" {
			t.Errorf("absent store %q should be skipped, not archived empty", n)
		}
	}
}

func TestTaskRun_InjectedShip(t *testing.T) {
	dir := writeTestStore(t)
	task, err := NewTask(Config{StateDir: dir, SSHHost: "testhost"}, nil)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	var shipped bytes.Buffer
	var shippedName string
	task.ship = func(_ context.Context, name string, archive io.Reader) error {
		shippedName = name
		_, err := io.Copy(&shipped, archive)
		return err
	}
	task.prune = func(context.Context) error { return nil }

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if shippedName == "" || filepath.Ext(shippedName) != ".gz" {
		t.Errorf("unexpected archive name %q", shippedName)
	}
	if !archiveNames(t, shipped.Bytes())["contacts.json"] {
		t.Error("shipped archive missing contacts.json")
	}
}

func TestNewTask_Validation(t *testing.T) {
	if _, err := NewTask(Config{SSHHost: "h"}, nil); err == nil {
		t.Error("missing StateDir must be rejected")
	}
	if _, err := NewTask(Config{StateDir: "/tmp"}, nil); err == nil {
		t.Error("missing SSHHost must be rejected")
	}
	if _, err := NewTask(Config{StateDir: "/tmp", SSHHost: "h", RemoteDir: "a;rm -rf"}, nil); err == nil {
		t.Error("shell metacharacters in RemoteDir must be rejected")
	}
}

// TestBackup_Live ships a tiny real archive over ssh and cleans it up.
// Requires the storage host to be reachable:
//
//	DENEB_BACKUP_LIVE=1 DENEB_BACKUP_SSH_HOST=spark4tb go test -run TestBackup_Live ./internal/domain/backup/
func TestBackup_Live(t *testing.T) {
	if os.Getenv("DENEB_BACKUP_LIVE") == "" {
		t.Skip("set DENEB_BACKUP_LIVE=1 for the live ssh test")
	}
	host := os.Getenv("DENEB_BACKUP_SSH_HOST")
	if host == "" {
		host = "spark4tb"
	}
	dir := writeTestStore(t)
	task, err := NewTask(Config{StateDir: dir, SSHHost: host, RemoteDir: "deneb-backups-livetest", RetentionDays: 1}, nil)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("live backup failed: %v", err)
	}
	out, err := exec.Command("ssh", "-o", "BatchMode=yes", host,
		"ls deneb-backups-livetest/ && rm -rf deneb-backups-livetest").CombinedOutput()
	if err != nil {
		t.Fatalf("remote verify/cleanup failed: %v (%s)", err, out)
	}
	if !bytes.Contains(out, []byte("deneb-memory-")) {
		t.Errorf("remote archive not found in listing: %s", out)
	}
}
