package tools

import "os/exec"

// requireRg skips the test if ripgrep (rg) is not installed.
// Several tool tests (ToolGrep, ToolSearchAndRead, etc.) shell out to rg
// and fail in environments where the binary is absent.
func requireRg(t interface {
	Helper()
	Skip(...any)
}) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg (ripgrep) not found in PATH; skipping")
	}
}
