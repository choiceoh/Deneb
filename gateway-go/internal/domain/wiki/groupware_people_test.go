package wiki

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestEnrichGroupwarePeople_CreatesStub(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	res, err := store.EnrichGroupwarePeople([]GroupwarePersonCard{{
		EmpCd:  "2019020101",
		Name:   "오선택",
		Dept:   "기획조정실",
		Div:    "탑솔라(주)",
		Title:  "전무",
		Mobile: "010-1111-2222",
		Birth:  "1993-01-29",
		Status: "재직",
	}})
	if err != nil {
		t.Fatalf("EnrichGroupwarePeople: %v", err)
	}
	if len(res.Links) != 1 || res.Links[0].Action != "created" {
		t.Fatalf("Links = %+v, want one created", res.Links)
	}
	page := testutil.Must(store.ReadPage(res.Links[0].Path))
	for _, want := range []string{
		"## 소속 · 직책", "기획조정실", "전무",
		"## 연락처", "010-1111-2222", "_아마란스 인사정보_",
		"## 비고", "생년월일: 1993-01-29", "사원코드: 2019020101",
	} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("missing %q in body:\n%s", want, page.Body)
		}
	}
	if strings.Contains(page.Body, "rsrgNo") || strings.Contains(page.Body, "주민") {
		t.Errorf("sensitive field leaked into page")
	}
}

func TestEnrichGroupwarePeople_UpdatesExisting(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	person := NewPage("김민준", "인물", nil)
	person.Body = "# 김민준\n\n## 메모\n기존 메모 유지.\n"
	if err := store.WritePage("인물/김민준.md", person); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	res, err := store.EnrichGroupwarePeople([]GroupwarePersonCard{{
		Name:      "김민준 부장",
		Dept:      "구매팀",
		Div:       "탑솔라(주)",
		Title:     "부장",
		Mobile:    "010-1234-5678",
		OfficeTel: "062-000-0000",
		Birth:     "1980-05-01",
		EmpCd:     "E001",
	}})
	if err != nil {
		t.Fatalf("EnrichGroupwarePeople: %v", err)
	}
	if len(res.Links) != 1 || res.Links[0].Action != "updated" {
		t.Fatalf("Links = %+v, want updated", res.Links)
	}
	got := testutil.Must(store.ReadPage("인물/김민준.md"))
	if !strings.Contains(got.Body, "## 메모") || !strings.Contains(got.Body, "기존 메모 유지") {
		t.Errorf("pre-existing section clobbered: %q", got.Body)
	}
	if !strings.Contains(got.Body, "010-1234-5678") || !strings.Contains(got.Body, "부장") {
		t.Errorf("enrichment missing: %q", got.Body)
	}
}

func TestEnrichGroupwarePeople_Idempotent(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	card := GroupwarePersonCard{
		Name:   "이서연",
		Dept:   "영업팀",
		Title:  "과장",
		Mobile: "010-9999-8888",
		Birth:  "1990-12-12",
		EmpCd:  "E002",
	}
	first, err := store.EnrichGroupwarePeople([]GroupwarePersonCard{card})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.Links[0].Action != "created" {
		t.Fatalf("first action = %s", first.Links[0].Action)
	}
	before := testutil.Must(store.ReadPage(first.Links[0].Path))

	second, err := store.EnrichGroupwarePeople([]GroupwarePersonCard{card})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Links[0].Action != "unchanged" {
		t.Fatalf("second action = %s, want unchanged", second.Links[0].Action)
	}
	after := testutil.Must(store.ReadPage(first.Links[0].Path))
	if before.Body != after.Body {
		t.Errorf("body churned on idempotent re-run")
	}
	if before.Meta.Updated != after.Meta.Updated {
		t.Errorf("Updated date churned: %q → %q", before.Meta.Updated, after.Meta.Updated)
	}
}

func TestEnrichGroupwarePeople_SkipsSensitiveFields(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	// JSON round-trip must not invent rsrgNo; card type has no such field.
	raw := `{"empCd":"X","name":"박지훈","dept":"개발","mobile":"010-0000-0000","birth":"1995-03-03"}`
	var card GroupwarePersonCard
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		t.Fatal(err)
	}
	res, err := store.EnrichGroupwarePeople([]GroupwarePersonCard{card})
	if err != nil {
		t.Fatal(err)
	}
	page := testutil.Must(store.ReadPage(res.Links[0].Path))
	lower := strings.ToLower(page.Body)
	for _, bad := range []string{"rsrgno", "주민등록", "주소록에서 동기화됨"} {
		if strings.Contains(lower, strings.ToLower(bad)) || strings.Contains(page.Body, bad) {
			t.Errorf("found forbidden %q", bad)
		}
	}
}
