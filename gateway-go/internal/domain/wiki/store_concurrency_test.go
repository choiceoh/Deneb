package wiki

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

// TestStore_RebuildIndex_SerializesAgainstWriters is the deterministic guard for
// the fix: RebuildIndex must hold writeMu across its disk scan + index swap so a
// concurrent page write can't land between them and have its index entry dropped
// by the wholesale swap. We simulate an in-flight writer by holding writeMu, then
// assert RebuildIndex BLOCKS until we release it. Without the writeMu acquisition
// (the pre-fix racy rebuild) RebuildIndex returns immediately while a writer
// "holds" the lock — exactly the window that drops a just-written entry — and
// this test fails. The transient drop itself is hard to catch in a fuzz test
// because a later rebuild re-scans the full disk and self-heals it; this asserts
// the structural invariant instead.
func TestStore_RebuildIndex_SerializesAgainstWriters(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	p := NewPage("Seed", "프로젝트", nil)
	p.Body = "# Seed\n\nbody"
	if err := store.WritePage("프로젝트/seed.md", p); err != nil {
		t.Fatalf("seed WritePage: %v", err)
	}

	// Stand in for a writer mid-flight: hold writeMu, the lock every page write
	// serializes on.
	store.writeMu.Lock()

	done := make(chan error, 1)
	go func() { done <- store.RebuildIndex() }()

	select {
	case <-done:
		// RebuildIndex finished while "a writer" held writeMu — it does not
		// serialize against writers, so a concurrent write's index update can be
		// clobbered by the swap.
		store.writeMu.Unlock()
		t.Fatal("RebuildIndex completed while writeMu was held; it must block on writeMu to snapshot disk consistently")
	case <-time.After(200 * time.Millisecond):
		// Expected: blocked on writeMu.
	}

	store.writeMu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RebuildIndex: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RebuildIndex did not complete after writeMu was released")
	}
}

// TestStore_RebuildIndexConcurrentWithWrites_IndexMatchesDisk hammers a full
// index rebuild against a writer creating brand-new pages and concurrent index
// readers — the dreamer's Phase-4 rebuild overlapping a wiki-research/mail-
// analysis write. This is -race coverage for the scan/swap boundary (s.mu) plus
// an eventual-consistency check: once every goroutine quiesces the last writeMu
// holder (a write or the final rebuild) leaves the cached index agreeing with
// disk in both directions — every on-disk page indexed, no ghost entries. (It
// does not deterministically reproduce the transient mid-flight drop — a later
// rebuild self-heals it; TestStore_RebuildIndex_SerializesAgainstWriters guards
// the invariant directly.) Run with -race.
func TestStore_RebuildIndexConcurrentWithWrites_IndexMatchesDisk(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	// Seed pages so each rebuild has real disk reads to do, widening the
	// scan→swap window a concurrent create could slip through.
	const seeds = 30
	for i := 0; i < seeds; i++ {
		p := NewPage(fmt.Sprintf("Seed %d", i), "프로젝트", nil)
		p.Body = fmt.Sprintf("# Seed %d\n\nbody", i)
		if err := store.WritePage(fmt.Sprintf("프로젝트/seed-%d.md", i), p); err != nil {
			t.Fatalf("seed WritePage: %v", err)
		}
	}

	const newPages = 40
	var wg sync.WaitGroup

	// Writer: creates brand-new pages, each of which updates the cached index.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < newPages; i++ {
			p := NewPage(fmt.Sprintf("New %d", i), "프로젝트", nil)
			p.Body = fmt.Sprintf("# New %d\n\nbody", i)
			if err := store.WritePage(fmt.Sprintf("프로젝트/new-%d.md", i), p); err != nil {
				t.Errorf("WritePage: %v", err)
				return
			}
		}
	}()

	// Rebuilder: rebuilds the master index repeatedly, racing the writer.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < newPages; i++ {
			if err := store.RebuildIndex(); err != nil {
				t.Errorf("RebuildIndex: %v", err)
				return
			}
		}
	}()

	// Readers stress the s.mu read/swap boundary for the race detector.
	for r := 0; r < 2; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < newPages; i++ {
				_ = store.Index().Entries
			}
		}()
	}
	wg.Wait()

	// Every page on disk must have a matching index entry: a dropped create (the
	// race) shows up as a page present on disk but missing from the cached index.
	onDisk := testutil.Must(store.ListPages(""))
	idx := store.Index()
	diskSet := make(map[string]bool, len(onDisk))
	for _, relPath := range onDisk {
		diskSet[relPath] = true
		entry, ok := idx.Entries[relPath]
		if !ok {
			t.Errorf("page %q on disk but missing from index", relPath)
			continue
		}
		page := testutil.Must(store.ReadPage(relPath))
		if entry.Title != page.Meta.Title {
			t.Errorf("index title %q != disk title %q for %q", entry.Title, page.Meta.Title, relPath)
		}
	}
	// And no index entry may reference a page that isn't on disk.
	for relPath := range idx.Entries {
		if !diskSet[relPath] {
			t.Errorf("index has ghost entry %q with no page on disk", relPath)
		}
	}
	// Sanity: all seeds + all new pages actually landed.
	if len(onDisk) != seeds+newPages {
		t.Errorf("page count on disk = %d, want %d", len(onDisk), seeds+newPages)
	}
}
