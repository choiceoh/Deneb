package wiki

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// aheadWorkspace writes a generated MEMORY.md/USER.md pair that already
// declares a revision far beyond anything the fresh store can hold, which is
// what a restored workspace or a swapped state dir leaves behind.
func aheadWorkspace(t *testing.T, dir string, revision int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := fmt.Sprintf("%s revision=%d; source=%s; do-not-edit -->\n", factGeneratedMarker, revision, factJournalFile)
	for _, name := range []string{"MEMORY.md", "USER.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(marker+"# "+name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// An ahead-of-store projection must be distinguishable from a broken fact
// plane: it costs nothing, so startup keeps wiki enabled instead of spending
// fail-closed restarts and then disabling the whole subsystem.
func TestSetFactProjectionDirReportsAheadProjectionAsItsOwnSentinel(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	aheadWorkspace(t, workspace, 126)

	store, err := NewStore(filepath.Join(root, "wiki"), filepath.Join(root, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	err = store.SetFactProjectionDir(workspace)
	if err == nil {
		t.Fatal("SetFactProjectionDir = nil, want the ahead-projection sentinel")
	}
	if !errors.Is(err, ErrFactProjectionAhead) {
		t.Fatalf("errors.Is(%v, ErrFactProjectionAhead) = false", err)
	}
	if !strings.Contains(err.Error(), "revision 126") {
		t.Fatalf("error lost the concrete revisions: %v", err)
	}
	// The sentinel text belongs in the message exactly once — the operator log
	// prints this verbatim.
	if n := strings.Count(err.Error(), ErrFactProjectionAhead.Error()); n != 1 {
		t.Fatalf("sentinel text appears %d times in %q, want 1", n, err.Error())
	}
	// The directory stays bound so writes resume by themselves once this
	// store's journal passes the on-disk revision. Rolling it back would make
	// the condition permanent.
	if got := store.factProjectionDir; got == "" {
		t.Fatal("projection dir was rolled back; the block can no longer clear itself")
	}
	if !bytesAheadUntouched(t, workspace, 126) {
		t.Fatal("the richer on-disk projection was overwritten")
	}
}

func bytesAheadUntouched(t *testing.T, dir string, revision int) bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	return generatedProjectionRevision(raw) == FactRevision(revision)
}

// A real write failure keeps the historical fail-closed behaviour: it is NOT
// the harmless sentinel, and it rolls the projection dir back.
func TestSetFactProjectionDirKeepsRealWriteFailuresFailClosed(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(filepath.Join(root, "wiki"), filepath.Join(root, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	store.factProjectionRename = func(string, string) error {
		return errors.New("disk is gone")
	}

	err = store.SetFactProjectionDir(workspace)
	if err == nil {
		t.Fatal("SetFactProjectionDir = nil, want the write failure")
	}
	if errors.Is(err, ErrFactProjectionAhead) {
		t.Fatalf("a real write failure matched the harmless sentinel: %v", err)
	}
	if store.factProjectionDir != "" {
		t.Fatalf("projection dir stayed bound after a write failure: %q", store.factProjectionDir)
	}
}
