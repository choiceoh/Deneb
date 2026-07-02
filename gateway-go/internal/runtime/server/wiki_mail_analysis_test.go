package server

import "testing"

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
		if got := mailAnalysisWikiPath(c.msgID, c.related); got != c.want {
			t.Errorf("%s:\n  mailAnalysisWikiPath(%q, %v)\n  = %q\n  want %q", c.name, c.msgID, c.related, got, c.want)
		}
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
