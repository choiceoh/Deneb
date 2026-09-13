package appupdate

import (
	"os"
	"path/filepath"
	"testing"
)

func writeApk(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("apk"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// The published APK is the only copy a phone can download and version.json is
// live release state, so the serve dir belongs under the state dir. A 2026-09-13
// disk sweep removed ~/.cache/deneb-apk along with the caches around it — which
// is exactly what that name invites — and the release series went backwards.
func TestApkDirPrefersTheStateDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DENEB_APK_DIR", "")

	state := filepath.Join(home, ".deneb", "apk")
	writeApk(t, state, "deneb-937-abc-fossRelease.apk")
	if got := denebApkDir(); got != state {
		t.Errorf("denebApkDir() = %q, want %q", got, state)
	}
}

// A gateway and a publisher can roll out in either order, so the old location
// still answers while it is the only one holding a build.
func TestApkDirFallsBackToTheLegacyCacheWhileItIsTheOnlyOneWithABuild(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DENEB_APK_DIR", "")

	legacy := filepath.Join(home, ".cache", "deneb-apk")
	writeApk(t, legacy, "deneb-936-def-fossRelease.apk")
	if got := denebApkDir(); got != legacy {
		t.Errorf("with only a legacy build, denebApkDir() = %q, want %q", got, legacy)
	}

	// Once the new location has a build it wins, even though the old one still
	// has files: the publisher has rolled out and the old copy is history.
	state := filepath.Join(home, ".deneb", "apk")
	writeApk(t, state, "deneb-937-abc-fossRelease.apk")
	if got := denebApkDir(); got != state {
		t.Errorf("after migration, denebApkDir() = %q, want %q", got, state)
	}
}

// Neither location populated must still name the new one — a fresh host has to
// publish somewhere, and it is not the cache.
func TestApkDirNamesTheStateDirectoryWhenNothingIsPublished(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DENEB_APK_DIR", "")
	want := filepath.Join(home, ".deneb", "apk")
	if got := denebApkDir(); got != want {
		t.Errorf("denebApkDir() = %q, want %q", got, want)
	}
}

func TestApkDirEnvOverrideWinsOverBoth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeApk(t, filepath.Join(home, ".cache", "deneb-apk"), "deneb-936-def-fossRelease.apk")
	writeApk(t, filepath.Join(home, ".deneb", "apk"), "deneb-937-abc-fossRelease.apk")

	override := t.TempDir()
	t.Setenv("DENEB_APK_DIR", override)
	if got := denebApkDir(); got != override {
		t.Errorf("denebApkDir() = %q, want the override %q", got, override)
	}
}
