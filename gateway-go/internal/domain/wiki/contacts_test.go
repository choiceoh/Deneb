package wiki

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// EnrichContacts must touch ONLY existing 인물 pages whose name matches a
// contact — never a non-인물 page, never an unmatched contact, and never a new
// page.
func TestEnrichContacts_MatchesExistingPeopleOnly(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	person := NewPage("김민준", "인물", []string{"탑솔라"})
	person.Body = "# 김민준\n\n## 메모\n탑솔라 구매팀 담당."
	if err := store.WritePage("인물/김민준.md", person); err != nil {
		t.Fatalf("WritePage person: %v", err)
	}
	// A non-인물 page must never be enriched even if a contact name collides.
	tech := NewPage("DGX Spark", "운영시스템", nil)
	if err := store.WritePage("운영시스템/dgx-spark.md", tech); err != nil {
		t.Fatalf("WritePage tech: %v", err)
	}

	payload := []byte(`{"contacts":[
		{"name":"김민준 부장","phones":["010-1234-5678"],"emails":["minjun@topsolar.kr"],"org":"탑솔라"},
		{"name":"DGX Spark","phones":["010-0000-0000"]},
		{"name":"낯선거래처","phones":["010-9999-9999"]}
	]}`)
	res, err := store.EnrichContacts(payload)
	if err != nil {
		t.Fatalf("EnrichContacts: %v", err)
	}
	if res.Total != 3 {
		t.Errorf("Total = %d, want 3", res.Total)
	}
	if res.Matched != 1 || res.Updated != 1 {
		t.Errorf("Matched/Updated = %d/%d, want 1/1", res.Matched, res.Updated)
	}
	if len(res.Names) != 1 || res.Names[0] != "김민준" {
		t.Errorf("Names = %v, want [김민준]", res.Names)
	}

	got := testutil.Must(store.ReadPage("인물/김민준.md"))
	if !strings.Contains(got.Body, "010-1234-5678") {
		t.Errorf("phone not written into page: %q", got.Body)
	}
	if !strings.Contains(got.Body, "minjun@topsolar.kr") {
		t.Errorf("email not written into page")
	}
	if !strings.Contains(got.Body, "## 연락처") {
		t.Errorf("연락처 section heading missing")
	}
	if !strings.Contains(got.Body, "## 메모") || !strings.Contains(got.Body, "탑솔라 구매팀") {
		t.Errorf("pre-existing section was clobbered: %q", got.Body)
	}

	// The non-인물 page must be byte-identical (no enrichment).
	techGot := testutil.Must(store.ReadPage("운영시스템/dgx-spark.md"))
	if strings.Contains(techGot.Body, "010-0000-0000") {
		t.Errorf("non-인물 page was enriched")
	}
}

// Re-running the same sync must be a no-op: same match, zero updates, and the
// page's Updated date and section count must not churn.
func TestEnrichContacts_Idempotent(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	person := NewPage("이서연", "인물", nil)
	person.Body = "## 메모\n비고."
	if err := store.WritePage("인물/이서연.md", person); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	payload := []byte(`{"contacts":[{"name":"이서연","phones":["010-2222-3333"]}]}`)

	r1, err := store.EnrichContacts(payload)
	if err != nil {
		t.Fatalf("first EnrichContacts: %v", err)
	}
	if r1.Updated != 1 {
		t.Fatalf("first sync Updated = %d, want 1", r1.Updated)
	}
	before := testutil.Must(store.ReadPage("인물/이서연.md"))

	r2, err := store.EnrichContacts(payload)
	if err != nil {
		t.Fatalf("second EnrichContacts: %v", err)
	}
	if r2.Matched != 1 {
		t.Errorf("second sync Matched = %d, want 1 (still matches)", r2.Matched)
	}
	if r2.Updated != 0 {
		t.Errorf("second sync Updated = %d, want 0 (idempotent)", r2.Updated)
	}
	after := testutil.Must(store.ReadPage("인물/이서연.md"))
	if before.Meta.Updated != after.Meta.Updated {
		t.Errorf("Updated date churned: %q -> %q", before.Meta.Updated, after.Meta.Updated)
	}
	if n := strings.Count(after.Body, "## 연락처"); n != 1 {
		t.Errorf("연락처 section count = %d, want 1 (no duplicate)", n)
	}
}

func TestNormalizePersonName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"김민준", "김민준"},
		{"김민준 부장", "김민준"},
		{"김민준부장", "김민준"},
		{"김민준 (탑솔라)", "김민준"},
		{"홍길동 대표이사", "홍길동"},
		{"홍길동님", "홍길동"},
		{"김대표님", "김대표"}, // 2-rune floor: must not collapse to "김"
		{"John Doe", "johndoe"},
		{"이수민", "이수민"}, // must stay distinct from "이수"
		{"", ""},
		{"민", "민"},
	}
	for _, c := range cases {
		if got := normalizePersonName(c.in); got != c.want {
			t.Errorf("normalizePersonName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUpsertSection(t *testing.T) {
	// Replace an existing section, preserve the others and their order.
	body := "서문 문단.\n\n## 메모\n기존 메모.\n\n## 연락처\n- 전화: old-number\n"
	out := upsertSection(body, "연락처", "- 전화: new-number")
	if !strings.Contains(out, "new-number") {
		t.Errorf("replacement not applied: %q", out)
	}
	if strings.Contains(out, "old-number") {
		t.Errorf("old section content survived: %q", out)
	}
	if !strings.Contains(out, "## 메모") || !strings.Contains(out, "기존 메모") {
		t.Errorf("unrelated section lost: %q", out)
	}
	if !strings.Contains(out, "서문 문단") {
		t.Errorf("preamble lost: %q", out)
	}
	if n := strings.Count(out, "## 연락처"); n != 1 {
		t.Errorf("duplicate heading: count = %d", n)
	}

	// Append when the section is absent.
	out2 := upsertSection("## 메모\n비고.\n", "연락처", "- 전화: 010")
	if !strings.Contains(out2, "## 연락처") || !strings.Contains(out2, "010") {
		t.Errorf("section not appended: %q", out2)
	}
	if !strings.Contains(out2, "## 메모") {
		t.Errorf("existing section lost on append: %q", out2)
	}
}
