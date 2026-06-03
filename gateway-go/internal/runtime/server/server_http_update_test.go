package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLatestPublishedApk(t *testing.T) {
	dir := t.TempDir()
	// Published builds across versions, including a hyphenated variant segment
	// and a codeless file that must be ignored.
	for _, name := range []string{
		"deneb-2.9.10-133-fossDebug.apk",
		"deneb-2.9.28-151-fossDebug.apk",
		"deneb-2.9.30-153-mailfix-fossDebug.apk", // newest; variant has a hyphen
		"deneb-debug.apk",                        // no code -> ignored
		"notes.txt",                              // not an apk
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// version.json's code is intentionally stale (151 < 153) to prove the disk
	// files win and version.json only contributes notes.
	if err := os.WriteFile(filepath.Join(dir, "version.json"),
		[]byte(`{"code":151,"notes":"release notes"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	m, ok := latestPublishedApk(dir)
	if !ok {
		t.Fatal("expected a published apk")
	}
	if m.Code != 153 {
		t.Fatalf("want highest code 153, got %d", m.Code)
	}
	if m.File != "deneb-2.9.30-153-mailfix-fossDebug.apk" {
		t.Fatalf("want newest file, got %q", m.File)
	}
	if m.Name != "2.9.30" {
		t.Fatalf("want name 2.9.30, got %q", m.Name)
	}
	if m.Notes != "release notes" {
		t.Fatalf("want notes from version.json, got %q", m.Notes)
	}
}

func TestLatestPublishedApkNoCandidates(t *testing.T) {
	if _, ok := latestPublishedApk(t.TempDir()); ok {
		t.Fatal("empty dir must yield no manifest")
	}
	if _, ok := latestPublishedApk(filepath.Join(t.TempDir(), "does-not-exist")); ok {
		t.Fatal("missing dir must yield no manifest")
	}
}

func TestDenebApkDirEnvOverride(t *testing.T) {
	t.Setenv("DENEB_APK_DIR", "/tmp/custom-apk")
	if got := denebApkDir(); got != "/tmp/custom-apk" {
		t.Fatalf("env override ignored: %q", got)
	}
}

func TestApkFilePatternRejectsNonApk(t *testing.T) {
	for _, name := range []string{
		"deneb-debug.apk",                // no numeric code
		"deneb-2.9.30-153-fossDebug.aab", // wrong extension
		"random.apk",                     // wrong prefix
		"deneb-2.9.30-fossDebug.apk",     // missing code segment
	} {
		if apkFilePattern.MatchString(name) {
			t.Errorf("pattern should reject %q", name)
		}
	}
	for _, name := range []string{
		"deneb-2.9.30-153-fossDebug.apk",
		"deneb-2.9.30-153-mailfix-fossDebug.apk",
		"deneb-2.9.2-125-playStoreDebug.apk",
	} {
		if !apkFilePattern.MatchString(name) {
			t.Errorf("pattern should accept %q", name)
		}
	}
}
