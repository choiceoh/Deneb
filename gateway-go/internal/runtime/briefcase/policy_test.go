package briefcase

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPolicyAllowsOnlyRunLocalFilesystemAndAllowlistedDevice(t *testing.T) {
	r, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Cleanup() })
	paths, _ := r.Paths()
	policy, err := NewPolicy(r, PolicyOptions{AllowedDeviceKinds: []string{"notify", "alarm"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(paths.Workspace, "input", "task.md"),
		filepath.Join(paths.Files, "source.pdf"),
		filepath.Join(paths.State, "transcripts", "session.jsonl"),
	} {
		if err := policy.CheckRead(path); err != nil {
			t.Errorf("read %s: %v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(paths.Artifacts, "report.pdf"),
		filepath.Join(paths.Logs, "run.jsonl"),
		filepath.Join(paths.Home, ".cache", "entry"),
	} {
		if err := policy.CheckWrite(path); err != nil {
			t.Errorf("write %s: %v", path, err)
		}
	}
	if err := policy.CheckDevice("notify"); err != nil {
		t.Fatalf("allowlisted device: %v", err)
	}

	for name, err := range map[string]error{
		"outside read":   policy.CheckRead(filepath.Join(t.TempDir(), "secret")),
		"relative write": policy.CheckWrite("artifacts/report.pdf"),
		"unclean write": policy.CheckWrite(paths.Workspace + string(filepath.Separator) + "folder" +
			string(filepath.Separator) + ".." + string(filepath.Separator) + "report.pdf"),
		"network": policy.CheckNetwork(),
		"exec":    policy.CheckExec(),
		"device":  policy.CheckDevice("send_sms"),
		"unknown": policy.Authorize(PolicyRequest{Operation: "mystery"}),
	} {
		if !errors.Is(err, ErrPolicyDenied) {
			t.Errorf("%s error = %v, want ErrPolicyDenied", name, err)
		}
	}
}

func TestPolicyRejectsSymlinkEscapeForExistingAndFuturePaths(t *testing.T) {
	r, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Cleanup() })
	paths, _ := r.Paths()
	policy, err := NewPolicy(r, PolicyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	link := filepath.Join(paths.Workspace, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, filepath.Join(link, "new", "file.txt")} {
		if err := policy.CheckWrite(path); !errors.Is(err, ErrPolicyDenied) {
			t.Errorf("write through symlink %s error = %v, want ErrPolicyDenied", path, err)
		}
		if err := policy.CheckRead(path); !errors.Is(err, ErrPolicyDenied) {
			t.Errorf("read through symlink %s error = %v, want ErrPolicyDenied", path, err)
		}
	}
	uncleanEscape := link + string(filepath.Separator) + ".." + string(filepath.Separator) + "file.txt"
	if err := policy.CheckWrite(uncleanEscape); !errors.Is(err, ErrPolicyDenied) {
		t.Errorf("unclean symlink path error = %v, want ErrPolicyDenied", err)
	}
}

func TestPolicyFailsAfterCleanupAndOnInsecureRoot(t *testing.T) {
	r, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := r.Paths()
	policy, err := NewPolicy(r, PolicyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if err := policy.CheckWrite(filepath.Join(paths.Artifacts, "x")); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("post-cleanup error = %v, want ErrPolicyDenied", err)
	}

	insecure, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = insecure.Cleanup() })
	insecurePaths, _ := insecure.Paths()
	if err := os.Chmod(insecurePaths.Logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPolicy(insecure, PolicyOptions{}); err == nil {
		t.Fatal("NewPolicy accepted group/world-accessible run directory")
	}
}
