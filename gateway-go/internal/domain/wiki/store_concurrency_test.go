package wiki

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestStore_ConcurrentUpdatePage_NoLostUpdate fires two writers that each append
// distinct lines to the SAME page via UpdatePage and asserts every line from both
// writers survives. Without per-page write serialization the two read-modify-write
// cycles interleave (read,read,write,write) and the later atomic temp+rename
// clobbers the earlier writer's append wholesale — a silently lost update. This is
// exactly the dreamer-vs-wiki-research overlap on a 프로젝트 page. Run with -race.
func TestStore_ConcurrentUpdatePage_NoLostUpdate(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	const relPath = "프로젝트/concurrent.md"
	base := NewPage("Concurrent", "프로젝트", nil)
	base.Body = "# Concurrent\n\n## 로그\n"
	if err := store.WritePage(relPath, base); err != nil {
		t.Fatalf("seed WritePage: %v", err)
	}

	const perWriter = 50
	writers := []string{"A", "B"}

	var wg sync.WaitGroup
	for _, w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				marker := fmt.Sprintf("MARK-%s-%d", w, i)
				err := store.UpdatePage(relPath, func(cur *Page) (*Page, error) {
					if cur == nil {
						return nil, fmt.Errorf("page vanished mid-test")
					}
					cur.Body += "\n- " + marker
					return cur, nil
				})
				if err != nil {
					t.Errorf("UpdatePage(%s): %v", marker, err)
					return
				}
			}
		}()
	}
	wg.Wait()

	got := testutil.Must(store.ReadPage(relPath))
	for _, w := range writers {
		for i := 0; i < perWriter; i++ {
			marker := fmt.Sprintf("MARK-%s-%d", w, i)
			if !strings.Contains(got.Body, marker) {
				t.Errorf("lost update: %q missing from final body", marker)
			}
		}
	}
	// Total appended lines must equal both writers' sum exactly — no drops, no dupes.
	if n := strings.Count(got.Body, "MARK-"); n != len(writers)*perWriter {
		t.Errorf("marker count = %d, want %d", n, len(writers)*perWriter)
	}

	// The cached index entry must reflect the page that's actually on disk.
	entry, ok := store.Index().Entries[relPath]
	if !ok {
		t.Fatal("page missing from index after concurrent updates")
	}
	if entry.Title != got.Meta.Title {
		t.Errorf("index title %q != disk title %q", entry.Title, got.Meta.Title)
	}
}

// TestStore_ConcurrentWritePage_IndexMatchesDisk hammers WritePage and reads on
// the SAME page from several goroutines. Two full-page writes are inherently
// last-writer-wins on the body, but the index entry must always agree with what
// actually landed on disk — writeMu makes the file write and the index update one
// atomic step, so the surviving write's body and its index entry can never come
// from two different writes. Run with -race to also catch index/backlink map races.
func TestStore_ConcurrentWritePage_IndexMatchesDisk(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	const relPath = "프로젝트/hammer.md"
	const iters = 100

	var wg sync.WaitGroup
	// Two writers with distinct importance values so the winning write is identifiable.
	for w := 0; w < 2; w++ {
		imp := 0.4 + 0.1*float64(w) // 0.4 / 0.5
		title := fmt.Sprintf("Writer-%d", w)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				p := NewPage(title, "프로젝트", nil)
				p.Meta.Importance = imp
				p.Body = fmt.Sprintf("# %s\n\nbody %d", title, i)
				if err := store.WritePage(relPath, p); err != nil {
					t.Errorf("WritePage: %v", err)
					return
				}
			}
		}()
	}
	// Concurrent readers to flush out races on the index/backlink maps.
	for r := 0; r < 2; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				_ = store.Index()
				_, _ = store.ReadPage(relPath)
			}
		}()
	}
	wg.Wait()

	// Final state: the index entry must match the page on disk.
	onDisk := testutil.Must(store.ReadPage(relPath))
	entry, ok := store.Index().Entries[relPath]
	if !ok {
		t.Fatal("page missing from index after concurrent writes")
	}
	if entry.Title != onDisk.Meta.Title {
		t.Errorf("index title %q != disk title %q", entry.Title, onDisk.Meta.Title)
	}
	if entry.Importance != onDisk.Meta.Importance {
		t.Errorf("index importance %v != disk importance %v", entry.Importance, onDisk.Meta.Importance)
	}
}
