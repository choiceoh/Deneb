package mailarchive

import "testing"

func att(name string) ArchivedAttachment {
	return ArchivedAttachment{Filename: name, Bytes: []byte("x")}
}

func names(atts []ArchivedAttachment) []string {
	out := make([]string, 0, len(atts))
	for _, a := range atts {
		out = append(out, a.Filename)
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSelectArchivedAttachments(t *testing.T) {
	all := []ArchivedAttachment{att("견적서.pdf"), att("계약서.docx"), att("운영관리사양서.xlsx")}

	cases := []struct {
		selector string
		want     []string
	}{
		{"", []string{"견적서.pdf", "계약서.docx", "운영관리사양서.xlsx"}}, // all
		{"2", []string{"계약서.docx"}},                           // 1-based index
		{"견적", []string{"견적서.pdf"}},                           // filename substring
		{"사양", []string{"운영관리사양서.xlsx"}},                      // substring mid-name
		{"99", nil},   // out-of-range index → none
		{"없는파일", nil}, // no substring match → none
	}
	for _, c := range cases {
		got := names(selectArchivedAttachments(all, c.selector))
		if !eq(got, c.want) {
			t.Errorf("selector %q: got %v, want %v", c.selector, got, c.want)
		}
	}
}

func TestSelectArchivedAttachments_CaseInsensitive(t *testing.T) {
	all := []ArchivedAttachment{att("Quotation-Final.PDF")}
	if got := names(selectArchivedAttachments(all, "quotation")); !eq(got, []string{"Quotation-Final.PDF"}) {
		t.Errorf("case-insensitive substring failed: got %v", got)
	}
}
