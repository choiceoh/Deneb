package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLatestPublishedApk(t *testing.T) {
	dir := t.TempDir()
	// Published builds, mixing the legacy deneb-<name>-<code>-... filenames with
	// the new code-only deneb-<code>-<sha>-... shape; the newest is a new-shape file
	// to prove the transition-compatible pattern picks it. Plus a hyphenated variant
	// and a codeless file that must be ignored.
	for _, name := range []string{
		"deneb-2.9.10-133-fossDebug.apk",         // legacy: name + code
		"deneb-2.9.28-151-fossDebug.apk",         // legacy
		"deneb-2.9.30-153-mailfix-fossDebug.apk", // legacy; variant has a hyphen
		"deneb-154-a1b2c3d4-fossRelease.apk",     // new shape, newest (code 154)
		"deneb-debug.apk",                        // no code -> ignored
		"notes.txt",                              // not an apk
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// version.json's code is intentionally stale (151 < 154). The disk files still
	// win for code/file, AND the stale notes must be suppressed: captioning the
	// newest build (154) with an older build's (151) notes is the "예전 패치노트가
	// 다시 올라온다" bug. A mismatched version.json contributes nothing.
	if err := os.WriteFile(filepath.Join(dir, "version.json"),
		[]byte(`{"code":151,"notes":"release notes"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	m, ok := latestPublishedApk(dir)
	if !ok {
		t.Fatal("expected a published apk")
	}
	if m.Code != 154 {
		t.Fatalf("want highest code 154, got %d", m.Code)
	}
	if m.File != "deneb-154-a1b2c3d4-fossRelease.apk" {
		t.Fatalf("want newest file, got %q", m.File)
	}
	if m.Notes != "" {
		t.Fatalf("stale version.json (code 151 != 153) must not supply notes, got %q", m.Notes)
	}
}

// When version.json describes the same build as the newest APK, its notes ride
// along — the normal published-build case.
func TestLatestPublishedApkNotesMatchCode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "deneb-2.9.30-153-fossDebug.apk"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "version.json"),
		[]byte(`{"code":153,"notes":"release notes"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m, ok := latestPublishedApk(dir)
	if !ok {
		t.Fatal("expected a published apk")
	}
	if m.Notes != "release notes" {
		t.Fatalf("matching version.json (code 153) must supply notes, got %q", m.Notes)
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
		"deneb-2.9.30-153-fossDebug.apk",         // legacy: name + code
		"deneb-2.9.30-153-mailfix-fossDebug.apk", // legacy; hyphenated variant
		"deneb-2.9.2-125-playStoreDebug.apk",     // legacy
		"deneb-154-a1b2c3d4-fossRelease.apk",     // new shape: code-only
		"deneb-187-nogit-fossDebug.apk",          // new shape: code-only, non-hex sha
	} {
		if !apkFilePattern.MatchString(name) {
			t.Errorf("pattern should accept %q", name)
		}
	}
}
