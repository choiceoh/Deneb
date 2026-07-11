package briefcase

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRootCreatesPrivateFreshTreeAndEnvironment(t *testing.T) {
	parent := t.TempDir()
	first, err := NewRunRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Cleanup() })
	second, err := NewRunRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Cleanup() })

	paths, err := first.Paths()
	if err != nil {
		t.Fatal(err)
	}
	other, _ := second.Paths()
	if paths.Root == other.Root {
		t.Fatalf("run roots are not fresh: %s", paths.Root)
	}
	for name, path := range map[string]string{
		"root": paths.Root, "home": paths.Home, "state": paths.State,
		"files": paths.Files, "workspace": paths.Workspace,
		"artifacts": paths.Artifacts, "logs": paths.Logs, "temp": paths.Temp,
	} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("%s: %v", name, statErr)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Errorf("%s mode = %v, want private directory 0700", name, info.Mode())
		}
	}

	env, err := first.Environment()
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"HOME":                          paths.Home,
		"DENEB_STATE_DIR":               paths.State,
		"DENEB_FILES_DIR":               paths.Files,
		"DENEB_BRIEFCASE_WORKSPACE_DIR": paths.Workspace,
		"DENEB_BRIEFCASE_ARTIFACTS_DIR": paths.Artifacts,
		"DENEB_BRIEFCASE_LOGS_DIR":      paths.Logs,
		"TMPDIR":                        paths.Temp,
	} {
		if got := env[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if env["DENEB_REDACT_SECRETS"] != "true" {
		t.Error("secret redaction must be forced on")
	}
	configPath := env["DENEB_CONFIG_PATH"]
	configInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if configInfo.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 0600", configInfo.Mode())
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{paths.Workspace, `"autoResume": false`, `"enabled": false`} {
		if !strings.Contains(string(config), want) {
			t.Errorf("isolated config missing %q:\n%s", want, config)
		}
	}
}

func TestRunRootEnvironmentOverlayAndFailClosedIsolation(t *testing.T) {
	r, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Cleanup() })
	paths, _ := r.Paths()
	base := []string{
		"HOME=/real/home",
		"DENEB_STATE_DIR=/real/state",
		"OPENAI_API_KEY=must-not-leak",
		"HTTP_PROXY=http://proxy.invalid",
		"LANG=ko_KR.UTF-8",
		"LC_ALL=C.UTF-8",
	}
	overlaid, err := r.Environ(base)
	if err != nil {
		t.Fatal(err)
	}
	overlayMap := environMap(overlaid)
	if overlayMap["HOME"] != paths.Home || overlayMap["DENEB_STATE_DIR"] != paths.State {
		t.Fatalf("isolated paths did not override base: %#v", overlayMap)
	}
	if overlayMap["OPENAI_API_KEY"] != "must-not-leak" {
		t.Error("Environ must preserve explicitly supplied audited variables")
	}

	isolated, err := r.IsolatedEnviron(base)
	if err != nil {
		t.Fatal(err)
	}
	isolatedMap := environMap(isolated)
	for _, forbidden := range []string{"OPENAI_API_KEY", "HTTP_PROXY"} {
		if _, ok := isolatedMap[forbidden]; ok {
			t.Errorf("ambient authority leaked into isolated environment: %s", forbidden)
		}
	}
	if isolatedMap["LANG"] != "ko_KR.UTF-8" || isolatedMap["LC_ALL"] != "C.UTF-8" {
		t.Fatalf("safe locale settings were lost: %#v", isolatedMap)
	}
	if isolatedMap["HOME"] != paths.Home {
		t.Fatalf("isolated HOME = %q, want %q", isolatedMap["HOME"], paths.Home)
	}
}

func TestRunRootCleanupIsIdempotentAndClosesRoot(t *testing.T) {
	parent := t.TempDir()
	r, err := NewRunRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := r.Paths()
	keep := filepath.Join(parent, "keep")
	if err := os.WriteFile(keep, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if err := r.Cleanup(); err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
	if _, err := os.Stat(paths.Root); !os.IsNotExist(err) {
		t.Fatalf("run root still exists: %v", err)
	}
	if got, err := os.ReadFile(keep); err != nil || string(got) != "safe" {
		t.Fatalf("cleanup touched sibling: got=%q err=%v", got, err)
	}
	if _, err := r.Paths(); !errors.Is(err, ErrRunRootClosed) {
		t.Fatalf("Paths after cleanup error = %v, want ErrRunRootClosed", err)
	}
	if _, err := r.Environment(); !errors.Is(err, ErrRunRootClosed) {
		t.Fatalf("Environment after cleanup error = %v, want ErrRunRootClosed", err)
	}
}

func TestRunRootRejectsNonDirectoryParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRunRoot(path); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("error = %v, want non-directory rejection", err)
	}
}

func environMap(entries []string) map[string]string {
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}
