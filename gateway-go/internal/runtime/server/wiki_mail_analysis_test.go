package server

import "testing"

// TestMailAnalysisWikiPath locks the per-project nesting: a mail the analyzer
// linked to a project is filed under that project's mail-analyses sub-folder
// (the reliable related-project signal), while an unlinked mail stays flat.
func TestMailAnalysisWikiPath(t *testing.T) {
	cases := []struct {
		name    string
		msgID   string
		related []string
		want    string
	}{
		{
			name:  "no related → flat",
			msgID: "abc", related: nil,
			want: "프로젝트/mail-analyses/abc.md",
		},
		{
			name:  "related project → nested under its sub-folder",
			msgID: "abc", related: []string{"프로젝트/해남-EPC.md"},
			want: "프로젝트/mail-analyses/해남-EPC/abc.md",
		},
		{
			name:  "first real project wins; mail-analyses + non-project entries skipped",
			msgID: "abc",
			related: []string{
				"프로젝트/mail-analyses/other.md", // under 프로젝트 but a raw mail → skip
				"인물/김부장.md",                   // not a project → skip
				"프로젝트/부산8호.md",                // first real project → wins
			},
			want: "프로젝트/mail-analyses/부산8호/abc.md",
		},
		{
			name:    "only non-project related → flat",
			msgID:   "abc",
			related: []string{"인물/김부장.md", "프로젝트/mail-analyses/x.md"},
			want:    "프로젝트/mail-analyses/abc.md",
		},
	}
	for _, c := range cases {
		if got := mailAnalysisWikiPath(c.msgID, c.related); got != c.want {
			t.Errorf("%s:\n  mailAnalysisWikiPath(%q, %v)\n  = %q\n  want %q", c.name, c.msgID, c.related, got, c.want)
		}
	}
}
