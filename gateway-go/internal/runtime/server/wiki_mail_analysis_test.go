package server

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// TestMailAnalysisWikiPath locks the per-project layout slot: a mail the
// analyzer linked to a project is filed under that project's 메일분석/ folder
// (the reliable related-project signal), while an unlinked mail lands in the
// category-level 프로젝트/메일분석/ bucket. Both the new in-folder 대표페이지 form
// and the legacy flat form must resolve the owning project.
func TestMailAnalysisWikiPath(t *testing.T) {
	cases := []struct {
		name    string
		msgID   string
		related []string
		want    string
	}{
		{
			name:  "no related → category-level unlinked bucket",
			msgID: "abc", related: nil,
			want: "프로젝트/메일분석/abc.md",
		},
		{
			name:  "related project (legacy flat 대표페이지) → project 메일분석 slot",
			msgID: "abc", related: []string{"프로젝트/해남-EPC.md"},
			want: "프로젝트/해남-EPC/메일분석/abc.md",
		},
		{
			name:  "related project (in-folder 대표페이지) → project 메일분석 slot",
			msgID: "abc", related: []string{"프로젝트/해남-EPC/대표.md"},
			want: "프로젝트/해남-EPC/메일분석/abc.md",
		},
		{
			name:  "first real project wins; raw-data + non-project entries skipped",
			msgID: "abc",
			related: []string{
				"프로젝트/메일분석/other.md",           // category-level raw mail → skip
				"프로젝트/mail-analyses/legacy.md", // legacy raw bucket → skip
				"인물/김부장.md",                    // not a project → skip
				"프로젝트/부산8호/대표.md",              // first real project → wins
			},
			want: "프로젝트/부산8호/메일분석/abc.md",
		},
		{
			name:    "only non-project related → unlinked bucket",
			msgID:   "abc",
			related: []string{"인물/김부장.md", "프로젝트/메일분석/x.md"},
			want:    "프로젝트/메일분석/abc.md",
		},
	}
	for _, c := range cases {
		// nil store = no existing pages → pure layout mapping.
		if got := mailAnalysisWikiPath(nil, c.msgID, c.related); got != c.want {
			t.Errorf("%s:\n  mailAnalysisWikiPath(%q, %v)\n  = %q\n  want %q", c.name, c.msgID, c.related, got, c.want)
		}
	}
}

// TestMailAnalysisWikiPath_ExistingPageWins locks the global msgID dedup
// (메일 1통 = 1페이지): when a page for the message already exists in ANY
// 메일분석 bucket — the analysis cache was bypassed, or a re-run resolved a
// different project — the write targets that page in place instead of minting
// a second page in another folder.
func TestMailAnalysisWikiPath_ExistingPageWins(t *testing.T) {
	store, err := wiki.NewStore(filepath.Join(t.TempDir(), "wiki"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	existing := wiki.MailAnalysisPagePath("영산고", "msg-1")
	page := wiki.NewPage("RE: 견적", "프로젝트", nil)
	page.Meta.ID = "msg-1"
	if err := store.WritePage(existing, page); err != nil {
		t.Fatal(err)
	}

	// Re-run resolves a DIFFERENT project → still the existing page.
	if got := mailAnalysisWikiPath(store, "msg-1", []string{"프로젝트/부산8호/대표.md"}); got != existing {
		t.Fatalf("existing page must win: got %q, want %q", got, existing)
	}
	// Re-run resolves no project → still the existing page (not the bucket).
	if got := mailAnalysisWikiPath(store, "msg-1", nil); got != existing {
		t.Fatalf("existing page must win over the unlinked bucket: got %q, want %q", got, existing)
	}
	// A different message is unaffected.
	if got := mailAnalysisWikiPath(store, "msg-2", nil); got != "프로젝트/메일분석/msg-2.md" {
		t.Fatalf("fresh msgID must map to the layout slot: got %q", got)
	}
}

// TestDirectProjectPages locks the 대표페이지 filter both layouts must pass and
// raw-data paths must fail.
func TestDirectProjectPages(t *testing.T) {
	got := directProjectPages([]string{
		"프로젝트/영산고/대표.md",          // new form → keep
		"프로젝트/영산고/대표.md",          // dup → dropped
		"프로젝트/부산8호.md",            // legacy flat → keep
		"프로젝트/영산고/로그.md",          // sub-page → skip
		"프로젝트/영산고/메일분석/abc.md",    // raw data → skip
		"프로젝트/거래/한빛전기.md",         // deal ledger → skip
		"프로젝트/mail-analyses/x.md", // legacy raw bucket → skip
		"인물/김부장.md",               // other category → skip
	})
	want := []string{"프로젝트/영산고/대표.md", "프로젝트/부산8호.md"}
	if len(got) != len(want) {
		t.Fatalf("directProjectPages = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("directProjectPages[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
