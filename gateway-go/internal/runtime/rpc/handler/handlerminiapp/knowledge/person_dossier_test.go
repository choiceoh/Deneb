package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// The dossier joins four independent sources: 인물 wiki identity, a Gmail
// rollup, phoneledger call/notification history (name-matched), and
// full-text wiki references. Each degrades alone — the test exercises the
// full join plus the email-only and unconfigured-Gmail shapes.
func TestPersonDossierJoinsMailCallsAndWiki(t *testing.T) {
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	c := &fakePeopleClient{
		searchFn: func(_ context.Context, q string, _ int) ([]gmail.MessageSummary, error) {
			if !strings.HasPrefix(q, "from:minji@acme.example") {
				t.Errorf("mail query = %q", q)
			}
			return []gmail.MessageSummary{
				{From: "Minji <minji@acme.example>", Subject: "견적 검토 요청", Date: now.Format(time.RFC3339)},
				{From: "Minji <minji@acme.example>", Subject: "Re: 견적 검토 요청", Date: now.Add(-time.Hour).Format(time.RFC3339)},
			}, nil
		},
	}

	personPage := &wiki.Page{
		Meta: wiki.Frontmatter{Title: "김민지", Category: "인물", Summary: "ACME 구매팀 부장"},
		Body: "## 연락처\n- 이메일: minji@acme.example\n",
	}
	store := &fakeMemoryStore{
		listPagesFn: func(category string) ([]string, error) {
			if category != "인물" {
				return nil, nil
			}
			return []string{"인물/김민지.md"}, nil
		},
		readPageFn: func(rel string) (*wiki.Page, error) {
			if rel == "인물/김민지.md" {
				return personPage, nil
			}
			return nil, os.ErrNotExist
		},
		querySearchFn: func(_ context.Context, q string, _ int, options wiki.QueryOptions) (wiki.SearchReport, error) {
			if q != "김민지" {
				t.Errorf("wiki search = %q", q)
			}
			if !options.ExcludeFactResults {
				t.Error("dossier wiki reference search must request page-only results")
			}
			return wiki.SearchReport{Results: []wiki.SearchResult{
				{Path: "@facts/fact-789.md", Content: "김민지 담당자는 현행 사실", FactID: "fact-789", SubjectID: "person:minji"},
				{Path: "프로젝트/거래/acme.md", Content: "김민지 부장과 단가 협의"},
				{Path: "인물/김민지.md", Content: "자기 자신 — 제외"},
			}}, nil
		},
	}

	ledgerDir := t.TempDir()
	day := time.Now().UTC().Format("2006-01-02")
	entry := struct {
		TS     string `json:"ts"`
		Type   string `json:"type"`
		Source string `json:"source"`
		Text   string `json:"text"`
	}{TS: time.Now().UTC().Format(time.RFC3339), Type: "call", Source: "김민지", Text: "부재중 통화 1통"}
	if err := jsonlstore.Append(filepath.Join(ledgerDir, day+".jsonl"), entry); err != nil {
		t.Fatal(err)
	}
	noise := struct {
		TS   string `json:"ts"`
		Type string `json:"type"`
		Text string `json:"text"`
	}{TS: time.Now().UTC().Format(time.RFC3339), Type: "notification", Text: "광고 문자"}
	if err := jsonlstore.Append(filepath.Join(ledgerDir, day+".jsonl"), noise); err != nil {
		t.Fatal(err)
	}

	deps := PeopleDeps{
		Client:         func() (PeopleClient, error) { return c, nil },
		WikiStore:      func() (MemorySearcher, error) { return store, nil },
		PhoneLedgerDir: ledgerDir,
	}
	h := personDossier(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.person.dossier", map[string]any{"email": "minji@acme.example"}))
	var got struct {
		Dossier PersonDossierOut `json:"dossier"`
	}
	decode(t, resp, &got)
	d := got.Dossier

	if d.Name != "김민지" || d.WikiPath != "인물/김민지.md" || d.WikiSummary != "ACME 구매팀 부장" {
		t.Fatalf("identity = %+v", d)
	}
	if d.MailCount != 2 || len(d.RecentSubjects) != 2 || !strings.Contains(d.LastMailAt, "2026-08-23") {
		t.Fatalf("mail = %+v", d)
	}
	if len(d.Calls) != 1 || d.Calls[0].Source != "김민지" {
		t.Fatalf("calls = %+v", d.Calls)
	}
	if len(d.WikiRefs) != 1 || d.WikiRefs[0].Path != "프로젝트/거래/acme.md" {
		t.Fatalf("wikiRefs = %+v", d.WikiRefs)
	}
}

// Email-only person, no wiki page, no Gmail configured: the dossier still
// answers with the ledger/wiki sections that can match by the given name.
func TestPersonDossierDegradesWithoutGmail(t *testing.T) {
	deps := PeopleDeps{} // no Client, no WikiStore, no ledger
	h := personDossier(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.person.dossier", map[string]any{"name": "김민지"}))
	var got struct {
		Dossier PersonDossierOut `json:"dossier"`
	}
	decode(t, resp, &got)
	if got.Dossier.Name != "김민지" || got.Dossier.MailCount != 0 || len(got.Dossier.Calls) != 0 {
		t.Fatalf("dossier = %+v", got.Dossier)
	}

	// Neither email nor a usable name → params error, not an empty dossier.
	resp = h(authedCtx(), reqWith(t, "miniapp.person.dossier", map[string]any{}))
	if resp.Error == nil {
		t.Fatal("expected params error for empty request")
	}
}
