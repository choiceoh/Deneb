package recall

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

func TestPersonMailConflict_MarksDomainChangeKeepsSameOrg(t *testing.T) {
	wikiEmails := []string{"2555151@kia.com"}
	if got := personMailConflict(wikiEmails, "이현성 <lee@other.co.kr>"); got == "" || !strings.Contains(got, "불일치") {
		t.Fatalf("different company must flag, got %q", got)
	}
	if got := personMailConflict(wikiEmails, "이현성 <2555151@kia.com>"); got != "" {
		t.Fatalf("same address must not flag, got %q", got)
	}
	if got := personMailConflict(wikiEmails, "이현성 <jaeminkim@kia.com>"); got != "" {
		t.Fatalf("same company domain must not flag, got %q", got)
	}
	if got := personMailConflict(nil, "이현성 <lee@other.co.kr>"); got != "" {
		t.Fatalf("empty wiki emails must not flag, got %q", got)
	}
}

func TestAttachWikiMailConflicts_KeepsBothAndDoesNotMediateScores(t *testing.T) {
	store := testWikiStore(t)
	person := wiki.NewPage("박수진", "인물", nil)
	person.Meta.Emails = []string{"park@oldco.com"}
	person.Body = "# 박수진\n\n## 연락처\n- park@oldco.com\n"
	if err := store.WritePage("인물/박수진.md", person); err != nil {
		t.Fatal(err)
	}
	mailPage := wiki.NewPage("RE: 담당 변경", "프로젝트", nil)
	mailPage.Body = "> From: 박수진 <park@newco.kr>\n\n박수진 이직 안내\n"
	mailPath := "프로젝트/demo/메일분석/mid@newco.kr.md"
	if err := store.WritePage(mailPath, mailPage); err != nil {
		t.Fatal(err)
	}

	ev := []recallEvidence{
		{Kind: "wiki", Source: "인물/박수진.md", Note: "title: 박수진", Score: 1.20},
		{Kind: "wiki", Source: mailPath, Note: "From: 박수진 <park@newco.kr>", Score: 0.95},
	}
	got := attachWikiMailConflicts(context.Background(), store, ev, false)
	if len(got) != 2 {
		t.Fatalf("len=%d, want both rows kept", len(got))
	}
	if got[0].Score != 1.20 || got[1].Score != 0.95 {
		t.Fatalf("scores must not be mediated: %+v", got)
	}
	if !strings.Contains(got[0].Note, wikiMailConflictMarker) || !strings.Contains(got[1].Note, wikiMailConflictMarker) {
		t.Fatalf("both notes must carry 불일치, got %#v", got)
	}
}

func TestAttachWikiMailConflicts_CuePullsMailAnalysisWithoutDroppingPerson(t *testing.T) {
	store := testWikiStore(t)
	person := wiki.NewPage("박수진", "인물", nil)
	person.Meta.Emails = []string{"park@oldco.com"}
	person.Body = "박수진 담당"
	if err := store.WritePage("인물/박수진.md", person); err != nil {
		t.Fatal(err)
	}
	mailPage := wiki.NewPage("박수진 이직", "프로젝트", nil)
	mailPage.Body = "> From: 박수진 <park@newco.kr>\n\n박수진 연락처 변경\n"
	mailPath := "프로젝트/demo/메일분석/mid@newco.kr.md"
	if err := store.WritePage(mailPath, mailPage); err != nil {
		t.Fatal(err)
	}

	ev := []recallEvidence{
		{Kind: "wiki", Source: "인물/박수진.md", Note: "title: 박수진", Score: 1.10, Query: "박수진"},
	}
	got := attachWikiMailConflicts(context.Background(), store, ev, true)
	if len(got) < 2 {
		t.Fatalf("cue turn must add the mail-analysis row, got %+v", got)
	}
	if got[0].Source != "인물/박수진.md" || got[0].Score != 1.10 {
		t.Fatalf("person row must stay first with its score, got %+v", got[0])
	}
	foundMail := false
	for _, row := range got {
		if row.Source == mailPath {
			foundMail = true
			if row.Score != 1.10 {
				t.Fatalf("pulled mail must share the person score (no winner), got %.2f", row.Score)
			}
			if !strings.Contains(row.Note, wikiMailConflictMarker) {
				t.Fatalf("pulled mail must carry 불일치, got %q", row.Note)
			}
		}
	}
	if !foundMail {
		t.Fatalf("expected mail-analysis %s in %+v", mailPath, got)
	}
}

func TestBuildRefiltersMailConflictPullThroughFactSubjectGuard(t *testing.T) {
	store := testWikiStore(t)
	person := wiki.NewPage("박수진", "인물", nil)
	person.Meta.Emails = []string{"park@oldco.com"}
	person.Body = "박수진 담당자"
	if err := store.WritePage("인물/박수진.md", person); err != nil {
		t.Fatal(err)
	}
	const betaValue = "BETA-CODE-771"
	mailPage := wiki.NewPage("박수진 담당 변경", "프로젝트", nil)
	mailPage.Body = "> From: 박수진 <park@newco.kr>\n\n박수진 안내 " + betaValue
	if err := store.WritePage("프로젝트/demo/메일분석/beta@newco.kr.md", mailPage); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertFact(wiki.FactInput{
		Subject: "project:beta", Key: "contract.code", Value: betaValue,
		Kind: wiki.FactKindContract, Authority: wiki.FactAuthorityDirectUser,
	}); err != nil {
		t.Fatal(err)
	}

	out, _ := Build(context.Background(), Params{SessionKey: "client:mail-fact", Message: "박수진 기억나?"}, Deps{Wiki: store}, nil)
	if !strings.Contains(out, "박수진") {
		t.Fatalf("person evidence disappeared: %q", out)
	}
	if strings.Contains(out, betaValue) || strings.Contains(out, "project:beta") ||
		strings.Contains(out, wikiMailConflictMarker) || strings.Contains(out, "park@newco.kr") {
		t.Fatalf("mail conflict pull bypassed unmatched-subject guard: %q", out)
	}
}

func testWikiStore(t *testing.T) *wiki.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
