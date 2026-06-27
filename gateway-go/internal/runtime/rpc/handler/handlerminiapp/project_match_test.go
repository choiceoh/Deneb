package handlerminiapp

import (
	"sort"
	"testing"
)

func TestProjectMatchKeysAndLink(t *testing.T) {
	keys := projectMatchKeys(
		"탑솔라",
		"프로젝트/탑솔라.md",
		"pl1-tps-sup-001",
		[]string{"프로젝트/거래/한빛전기.md", "프로젝트/mail-analyses/탑솔라/abc123.md"},
	)

	cases := []struct {
		name string
		refs []string
		want bool
	}{
		{"by project path", []string{"프로젝트/탑솔라.md"}, true},
		{"by project path leaf", []string{"탑솔라"}, true},
		{"by frozen code", []string{"PL1-TPS-SUP-001"}, true}, // case-insensitive
		{"by owned deal page (graph ref)", []string{"프로젝트/거래/한빛전기.md"}, true},
		{"by deal page leaf", []string{"한빛전기"}, true},
		{"windows separators tolerated", []string{`프로젝트\거래\한빛전기`}, true},
		{"unrelated", []string{"프로젝트/영산고.md"}, false},
		{"empty", []string{"", "  "}, false},
	}
	for _, c := range cases {
		if got := itemLinkedToProject(keys, c.refs...); got != c.want {
			t.Errorf("%s: itemLinkedToProject(%v) = %v, want %v", c.name, c.refs, got, c.want)
		}
	}
}

func TestMailIDsFromRefs(t *testing.T) {
	refs := []string{
		"프로젝트/mail-analyses/탑솔라/abc123.md", // → abc123
		"프로젝트/mail-analyses/탑솔라/def456.md", // → def456
		`프로젝트\mail-analyses\탑솔라\win789.md`, // windows separators → win789
		"프로젝트/mail-analyses/탑솔라/abc123.md", // dup → dropped
		"프로젝트/거래/한빛전기.md",                  // not a mail analysis → skipped
		"프로젝트/탑솔라/이력.md",                   // skipped
	}
	got := mailIDsFromRefs(refs)
	sort.Strings(got)
	want := []string{"abc123", "def456", "win789"}
	if len(got) != len(want) {
		t.Fatalf("mailIDsFromRefs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mailIDsFromRefs = %v, want %v", got, want)
		}
	}
	if len(mailIDsFromRefs(nil)) != 0 {
		t.Error("mailIDsFromRefs(nil) should be empty")
	}
}
