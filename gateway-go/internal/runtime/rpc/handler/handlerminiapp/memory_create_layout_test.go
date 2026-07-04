// memory_create_layout_test.go — miniapp.memory.create_page layout guards:
// taxonomy category validation and the flat-project normalization
// (wiki-layout 불변식) added by the 2026-07 curation audit.
package handlerminiapp

import (
	"io/fs"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// TestMemoryCreatePage_RejectsUnknownCategory: create now enforces the same
// taxonomy-category guard move already had — a typo category must not spawn a
// junk top-level bucket.
func TestMemoryCreatePage_RejectsUnknownCategory(t *testing.T) {
	h := memoryCreatePage(memoryDepsFor(&fakeMemoryStore{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.create_page", map[string]any{
		"title":    "Acme",
		"category": "회사", // not in wiki.Categories
	}))
	if resp.OK {
		t.Fatal("expected error for unknown category")
	}
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("code = %s, want INVALID_REQUEST", resp.Error.Code)
	}
}

// TestMemoryCreatePage_ProjectCreateLandsOnRepSlot: creating under 프로젝트 must
// land on the 대표.md layout slot, not resurrect a flat 프로젝트/<slug>.md page.
func TestMemoryCreatePage_ProjectCreateLandsOnRepSlot(t *testing.T) {
	var capturedPath string
	store := &fakeMemoryStore{
		readPageFn: func(_ string) (*wiki.Page, error) { return nil, fs.ErrNotExist },
		writePageFn: func(p string, _ *wiki.Page) error {
			capturedPath = p
			return nil
		},
	}
	h := memoryCreatePage(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.create_page", map[string]any{
		"title":    "신규 태양광",
		"category": "프로젝트",
	}))
	if !resp.OK {
		t.Fatalf("expected OK: %+v", resp.Error)
	}
	if want := wiki.RepPagePath("신규-태양광"); capturedPath != want {
		t.Errorf("path = %q, want the layout slot %q", capturedPath, want)
	}
}
