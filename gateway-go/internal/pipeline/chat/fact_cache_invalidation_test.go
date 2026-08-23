package chat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
)

func TestFactAwareContextLoadSuppressesOnlyDegradedProjections(t *testing.T) {
	oldPersister := promptSnapshots
	promptSnapshots = &promptSnapshotPersister{logger: discardLogger()}
	prompt.ResetContextFileCacheForTest()
	t.Cleanup(func() {
		prompt.ResetContextFileCacheForTest()
		promptSnapshots = oldPersister
	})

	workspace := t.TempDir()
	for name, content := range map[string]string{
		"SOUL.md":   "stable soul",
		"USER.md":   "revision 3 user",
		"MEMORY.md": "revision 3 memory",
	} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	SetFactDerivedRevision(3)
	healthy := loadFactAwareContextFiles(workspace, "client:main:healthy")
	assertContextFilePresence(t, healthy, map[string]bool{
		"SOUL.md": true, "USER.md": true, "MEMORY.md": true,
	})

	ClearFactDerivedCachesAtRevision(4, "workspace projection failed")
	degraded := loadFactAwareContextFiles(workspace, "client:main:degraded")
	assertContextFilePresence(t, degraded, map[string]bool{
		"SOUL.md": true, "USER.md": false, "MEMORY.md": false,
	})

	for name, content := range map[string]string{
		"USER.md":   "revision 5 user",
		"MEMORY.md": "revision 5 memory",
	} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	SetFactDerivedRevision(5)
	repaired := loadFactAwareContextFiles(workspace, "client:main:repaired")
	assertContextFilePresence(t, repaired, map[string]bool{
		"SOUL.md": true, "USER.md": true, "MEMORY.md": true,
	})
}

func assertContextFilePresence(t *testing.T, files []prompt.ContextFile, expected map[string]bool) {
	t.Helper()
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		seen[filepath.Base(filepath.Clean(file.Path))] = true
	}
	for name, want := range expected {
		if seen[name] != want {
			t.Fatalf("context file %s presence=%v, want %v; files=%+v", name, seen[name], want, files)
		}
	}
}
