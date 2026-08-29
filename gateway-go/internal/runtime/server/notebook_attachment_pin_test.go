package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
)

// newArchiveStore builds a local file store rooted at a temp dir and drops one
// archived attachment into a dated mail-archive folder, the way
// mailanalysis.archiveAttachments does (<sender>_<filename>).
func newArchiveStore(t *testing.T, day, sender, filename string, body []byte) *filestore.LocalStore {
	t.Helper()
	root := t.TempDir()
	t.Setenv("DENEB_FILES_DIR", root)
	dir := filepath.Join(root, "Deneb-Archive", "메일", day)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sender+"_"+filename), body, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := filestore.DefaultLocalStore()
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// TestFindArchivedAttachmentMatchesTheSenderPrefixedName pins the lookup the
// attachment pin depends on: archiveAttachments writes "<sender>_<filename>"
// with both parts sanitized, so the notebook side matches on the filename
// suffix rather than re-deriving another package's sanitizer.
func TestFindArchivedAttachmentMatchesTheSenderPrefixedName(t *testing.T) {
	store := newArchiveStore(t, "2026-08-30", "김대희 <kdh@topsolar.kr>", "청송태양광 견적.xlsx", []byte("x"))

	got, ok := findArchivedAttachment(context.Background(), store, "청송태양광 견적.xlsx")
	if !ok {
		t.Fatal("archived attachment should resolve by filename suffix")
	}
	if want := "/Deneb-Archive/메일/2026-08-30/김대희 <kdh@topsolar.kr>_청송태양광 견적.xlsx"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

// TestFindArchivedAttachmentIgnoresOlderThanTwoDays keeps the scan bounded: the
// pin runs in the same cycle that archived the file, so today and yesterday are
// the whole search space. An unbounded walk over months of archive would be a
// per-mail cost for nothing.
func TestFindArchivedAttachmentIgnoresOlderThanTwoDays(t *testing.T) {
	store := newArchiveStore(t, "2026-08-30", "sender", "recent.xlsx", []byte("x"))
	root := store.Root()
	for _, day := range []string{"2026-08-29", "2026-08-20"} {
		dir := filepath.Join(root, "Deneb-Archive", "메일", day)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sender_old.xlsx"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 08-29 is the second-newest folder and stays in range.
	if _, ok := findArchivedAttachment(context.Background(), store, "old.xlsx"); !ok {
		t.Error("yesterday's folder must still be searched")
	}
	// Push 08-29 out of the window with a newer folder; 08-20 must stay unreachable.
	dir := filepath.Join(root, "Deneb-Archive", "메일", "2026-08-31")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, ok := findArchivedAttachment(context.Background(), store, "old.xlsx"); ok {
		t.Error("a folder older than the two most recent must not be searched")
	}
}

// TestFindArchivedAttachmentMissIsQuiet: a mail whose attachment never archived
// (too small, download failed) must resolve to nothing rather than erroring —
// the pin is best-effort and must never fail the analysis cycle.
func TestFindArchivedAttachmentMissIsQuiet(t *testing.T) {
	store := newArchiveStore(t, "2026-08-30", "sender", "present.pdf", []byte("x"))
	if _, ok := findArchivedAttachment(context.Background(), store, "absent.pdf"); ok {
		t.Fatal("a missing attachment must not resolve")
	}
}
