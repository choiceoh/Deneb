package wiki

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSearchAndSearchPlanReturnOnlyCanonicalActiveFactWinner(t *testing.T) {
	root := t.TempDir()
	wikiDir, diaryDir := filepath.Join(root, "wiki"), filepath.Join(root, "diary")
	store, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	oldValue, currentValue := "8120만원", "9350만원"
	oldPage := NewPage("alpha 이전 견적", "프로젝트", nil)
	oldPage.Body = "alpha quote amount " + oldValue + "\n\n정정 전 계약 자료"
	if err := store.WritePage("프로젝트/alpha-old.md", oldPage); err != nil {
		t.Fatal(err)
	}
	for index, value := range []string{oldValue, currentValue} {
		result, err := store.UpsertFact(FactInput{
			Subject: "project:alpha", Key: "quote.amount", Value: value,
			Kind: FactKindAmount, Authority: FactAuthorityPrimaryDoc,
			Sources: []string{"doc:alpha-quote-v" + string(rune('1'+index))},
			BasisAt: base.Add(time.Duration(index) * time.Hour),
			At:      base.Add(time.Duration(index) * time.Hour),
		})
		if err != nil || !result.Committed {
			t.Fatalf("UpsertFact(%q) = %+v, err=%v", value, result, err)
		}
	}
	if _, err := store.UpsertFact(FactInput{
		Subject: "project:beta", Key: "quote.amount", Value: "7777만원",
		Kind: FactKindAmount, Authority: FactAuthorityPrimaryDoc,
		Sources: []string{"doc:beta"}, BasisAt: base, At: base,
	}); err != nil {
		t.Fatal(err)
	}

	assertCurrent := func(label string, results []SearchResult) {
		t.Helper()
		if len(results) == 0 || results[0].FactID == "" {
			t.Fatalf("%s did not return a fact-plane winner: %+v", label, results)
		}
		joined := ""
		for _, result := range results {
			joined += "\n" + result.Content + "\n" + result.ExpandedContent
			if result.Path == "프로젝트/alpha-old.md" {
				t.Fatalf("%s returned ordinary page containing a superseded value: %+v", label, result)
			}
			if result.FactID != "" && result.SubjectID != "project:alpha" {
				t.Fatalf("%s leaked unrelated fact subject: %+v", label, result)
			}
		}
		if !strings.Contains(joined, currentValue) || strings.Contains(joined, oldValue) || strings.Contains(joined, "7777만원") {
			t.Fatalf("%s fact results violated lifecycle/isolation:\n%s", label, joined)
		}
	}

	results, err := store.Search(context.Background(), "alpha 견적 금액", 5)
	if err != nil {
		t.Fatal(err)
	}
	assertCurrent("Search", results)
	report, err := store.SearchPlan(context.Background(), QueryPlan{
		Clauses: []QueryClause{
			{Kind: QueryKindLex, Query: "unrelated meeting notes"},
			{Kind: QueryKindLex, Query: "alpha 견적 금액"},
		},
	}, 5)
	if err != nil {
		t.Fatal(err)
	}
	assertCurrent("SearchPlan", report.Results)

	unrelated, err := store.Search(context.Background(), "alpha 지난 회의 참석자", 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range unrelated {
		if result.FactID != "" {
			t.Fatalf("unrelated aspect admitted an amount fact: %+v", result)
		}
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, err := reopened.Search(context.Background(), "alpha 견적 금액", 5)
	if err != nil {
		t.Fatal(err)
	}
	assertCurrent("Search after restart", restarted)
}

func TestTypedFactLifecycleKeepsSharedShortValuesScopedAcrossSearchPlanAndRestart(t *testing.T) {
	root := t.TempDir()
	wikiDir, diaryDir := filepath.Join(root, "wiki"), filepath.Join(root, "diary")
	store, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)

	write := func(path, title, subject, body string) {
		t.Helper()
		page := NewPage(title, "프로젝트", nil)
		page.Meta.SubjectID = subject
		page.Body = body
		if writeErr := store.WritePage(path, page); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	write("자료/quality-grade.md", "품질 등급", "", "품질 등급 A는 통과")
	write("프로젝트/alpha-quote.md", "alpha quote", "project:alpha", "project alpha quote amount A")
	write("프로젝트/beta-quote-old.md", "beta old quote", "project:beta", "project beta quote amount A")
	write("프로젝트/beta-quote-current.md", "beta current quote", "project:beta", "project beta quote amount B")

	upsert := func(input FactInput) {
		t.Helper()
		result, upsertErr := store.UpsertFact(input)
		if upsertErr != nil || !result.Committed {
			t.Fatalf("UpsertFact(%s/%s/%s) = %+v, err=%v", input.Subject, input.Key, input.Value, result, upsertErr)
		}
	}
	for index, value := range []string{"A", "B"} {
		upsert(FactInput{
			Subject: "self", Key: "preference.language", Value: value,
			Kind: FactKindPreference, Authority: FactAuthorityDirectUser,
			At: base.Add(time.Duration(index) * time.Hour),
		})
	}
	upsert(FactInput{
		Subject: "project:alpha", Key: "quote.amount", Value: "A",
		Kind: FactKindAmount, Authority: FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha"}, At: base, BasisAt: base,
	})
	for index, value := range []string{"A", "B"} {
		at := base.Add(time.Duration(index) * time.Hour)
		upsert(FactInput{
			Subject: "project:beta", Key: "quote.amount", Value: value,
			Kind: FactKindAmount, Authority: FactAuthorityPrimaryDoc,
			Sources: []string{"doc:beta"}, At: at, BasisAt: at,
		})
	}

	assertScoped := func(label string, current *Store) {
		t.Helper()
		quality, searchErr := current.Search(context.Background(), "품질 등급", 10)
		if searchErr != nil || !searchResultsContainPath(quality, "자료/quality-grade.md") {
			t.Fatalf("%s unrelated shared A missing from Search: %+v err=%v", label, quality, searchErr)
		}
		qualityPlan, planErr := current.SearchPlan(context.Background(), QueryPlan{
			Clauses: []QueryClause{{Kind: QueryKindLex, Query: "품질 등급"}},
		}, 10)
		if planErr != nil || !searchResultsContainPath(qualityPlan.Results, "자료/quality-grade.md") {
			t.Fatalf("%s unrelated shared A missing from SearchPlan: %+v err=%v", label, qualityPlan.Results, planErr)
		}

		alpha, searchErr := current.Search(context.Background(), "project alpha quote amount", 10)
		if searchErr != nil || !searchResultsContainPath(alpha, "프로젝트/alpha-quote.md") {
			t.Fatalf("%s alpha current A was removed: %+v err=%v", label, alpha, searchErr)
		}
		beta, searchErr := current.Search(context.Background(), "project beta quote amount", 10)
		if searchErr != nil {
			t.Fatal(searchErr)
		}
		if searchResultsContainPath(beta, "프로젝트/beta-quote-old.md") ||
			!searchResultsContainPath(beta, "프로젝트/beta-quote-current.md") {
			t.Fatalf("%s beta lifecycle mismatch: %+v", label, beta)
		}
		joined := ""
		for _, hit := range beta {
			joined += "\n" + hit.Content + "\n" + hit.ExpandedContent
		}
		if strings.Contains(joined, "amount A") || !strings.Contains(joined, "amount B") {
			t.Fatalf("%s beta stale/current values mismatch:\n%s", label, joined)
		}
	}

	snapshot := store.RecallFactSnapshot()
	if strings.Contains(strings.Join(snapshot.StaleValues, "\n"), "A") {
		t.Fatalf("typed short value leaked into global stale lines: %+v", snapshot.StaleValues)
	}
	if len(snapshot.LifecycleRules) != 2 {
		t.Fatalf("lifecycle rules = %+v, want self and beta corrections", snapshot.LifecycleRules)
	}
	assertScoped("live", store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertScoped("reopen", reopened)
}

func TestPageSearchIgnoresUntypedSupersededLinesButKeepsTypedLifecycle(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	const (
		sharedLine   = "선택님 6월 19일 확인 완료"
		currentPath  = "업무/파이프라인-현행.md"
		oldPath      = "업무/파이프라인-구버전.md"
		typedOldPath = "프로젝트/beta-quote-old.md"
		typedNewPath = "프로젝트/beta-quote-current.md"
	)

	write := func(path, title, subject, body string) {
		t.Helper()
		page := NewPage(title, "업무", nil)
		page.Meta.SubjectID = subject
		page.Body = body
		if err := store.WritePage(path, page); err != nil {
			t.Fatal(err)
		}
	}
	write(oldPath, "착공 파이프라인 구버전", "", "옛 파이프라인 기준점 411MW\n"+sharedLine)
	write(currentPath, "착공 파이프라인 현행", "", "현행 파이프라인 기준점 534MW\n"+sharedLine)
	write(typedOldPath, "beta quote old", "project:beta", "project beta quote amount 8120만원")
	write(typedNewPath, "beta quote current", "project:beta", "project beta quote amount 9350만원")
	if err := store.MarkSuperseded(oldPath, currentPath); err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	for index, value := range []string{"8120만원", "9350만원"} {
		at := base.Add(time.Duration(index) * time.Hour)
		result, err := store.UpsertFact(FactInput{
			Subject: "project:beta", Key: "quote.amount", Value: value,
			Kind: FactKindAmount, Authority: FactAuthorityPrimaryDoc,
			Sources: []string{"doc:beta"}, BasisAt: at, At: at,
		})
		if err != nil || !result.Committed {
			t.Fatalf("UpsertFact(%q) = %+v, err=%v", value, result, err)
		}
	}

	snapshot := store.RecallFactSnapshot()
	if containsString(snapshot.StaleValues, sharedLine) {
		t.Fatalf("current successor line remained globally stale: %+v", snapshot.StaleValues)
	}
	if len(snapshot.LifecycleRules) != 1 {
		t.Fatalf("typed lifecycle rules = %+v, want beta correction", snapshot.LifecycleRules)
	}

	searchPages := func(query string) []SearchResult {
		t.Helper()
		report, err := store.SearchWithOptions(context.Background(), query, 10, QueryOptions{
			Mode: SearchModeBM25, SkipRerank: true, ExcludeFactResults: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return report.Results
	}
	current := searchPages("현행 파이프라인 기준점")
	if !searchResultsContainPath(current, currentPath) {
		t.Fatalf("shared untyped stale line removed the current page: %+v", current)
	}
	if old := searchPages("옛 파이프라인 기준점"); searchResultsContainPath(old, oldPath) {
		t.Fatalf("superseded page resurfaced after page-line exemption: %+v", old)
	}
	typed := searchPages("project beta quote amount")
	if searchResultsContainPath(typed, typedOldPath) || !searchResultsContainPath(typed, typedNewPath) {
		t.Fatalf("typed lifecycle filtering changed: %+v", typed)
	}
}

func TestFactLifecycleMatcherRequiresIdentityForShortTypedAndUntypedValues(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	snapshot := FactRecallSnapshot{
		Active: []FactClaim{
			{Subject: "project:alpha", Key: "quote.total", Kind: FactKindAmount, Value: "A", Status: FactStatusCurrent},
			{Subject: "project:beta", Key: "quote.total", Kind: FactKindAmount, Value: "B", Status: FactStatusCurrent},
			{Subject: "project:beta", Key: "budget.limit", Kind: FactKindAmount, Value: "C", Status: FactStatusCurrent},
		},
		LifecycleRules: []FactLifecycleRule{{
			Subject: "project:beta", Key: "quote.total", Kind: FactKindAmount,
			CurrentValues: []string{"B"}, StaleValues: []string{"A"},
		}},
		StaleValues: []string{"A", "오로라 운영 포트는 18789를 사용한다"},
	}
	tests := []struct {
		name     string
		evidence FactLifecycleEvidence
		want     bool
	}{
		{
			name: "other subject current shared value",
			evidence: FactLifecycleEvidence{
				Query: "project beta quote amount", Ref: "project/alpha/quote.txt",
				Text: "project alpha quote amount A",
			},
			want: true,
		},
		{
			name: "broad subject old short value",
			evidence: FactLifecycleEvidence{
				Query: "project beta", Ref: "project/beta/history.txt", Text: "project beta was A",
			},
			want: false,
		},
		{
			name: "unrelated short typed and untyped value",
			evidence: FactLifecycleEvidence{
				Query: "품질 등급", Ref: "quality/grade.txt", Text: "품질 등급 A는 통과",
			},
			want: true,
		},
		{
			name: "kind alone is ambiguous across active keys",
			evidence: FactLifecycleEvidence{
				Query: "project beta amount", Ref: "project/beta/overview.txt", Text: "project beta amount is pending",
			},
			want: true,
		},
		{
			name: "long legacy phrase remains globally denied",
			evidence: FactLifecycleEvidence{
				Query: "운영 포트", Ref: "notes/copy.txt", Text: "오로라 운영 포트는 18789를 사용한다",
			},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := store.FactLifecycleEvidenceAllowed(test.evidence, snapshot); got != test.want {
				t.Fatalf("FactLifecycleEvidenceAllowed(%+v) = %v, want %v", test.evidence, got, test.want)
			}
		})
	}
}

func TestTombstonedShortValueFiltersOnlyItsIdentityAcrossRestart(t *testing.T) {
	root := t.TempDir()
	wikiDir, diaryDir := filepath.Join(root, "wiki"), filepath.Join(root, "diary")
	store, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	old := NewPage("gamma old status", "프로젝트", nil)
	old.Meta.SubjectID = "project:gamma"
	old.Body = "project gamma release status A"
	if err := store.WritePage("프로젝트/gamma-old-status.md", old); err != nil {
		t.Fatal(err)
	}
	unrelated := NewPage("품질 등급", "자료", nil)
	unrelated.Body = "품질 등급 A는 통과"
	if err := store.WritePage("자료/quality-grade.md", unrelated); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	if _, err := store.UpsertFact(FactInput{
		Subject: "project:gamma", Key: "release.status", Value: "A",
		Kind: FactKindGeneric, Authority: FactAuthorityAgent, At: base,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TombstoneFact(FactTombstoneInput{
		Subject: "project:gamma", Key: "release.status",
		Authority: FactAuthorityAgent, At: base.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	assertScoped := func(label string, current *Store) {
		t.Helper()
		oldHits, searchErr := current.Search(context.Background(), "project gamma release status", 10)
		if searchErr != nil || searchResultsContainPath(oldHits, "프로젝트/gamma-old-status.md") {
			t.Fatalf("%s tombstoned identity resurfaced: %+v err=%v", label, oldHits, searchErr)
		}
		plan, planErr := current.SearchPlan(context.Background(), QueryPlan{
			Clauses: []QueryClause{{Kind: QueryKindLex, Query: "project gamma release status"}},
		}, 10)
		if planErr != nil || searchResultsContainPath(plan.Results, "프로젝트/gamma-old-status.md") {
			t.Fatalf("%s tombstoned identity resurfaced in plan: %+v err=%v", label, plan.Results, planErr)
		}
		quality, searchErr := current.Search(context.Background(), "품질 등급", 10)
		if searchErr != nil || !searchResultsContainPath(quality, "자료/quality-grade.md") {
			t.Fatalf("%s unrelated shared A was removed: %+v err=%v", label, quality, searchErr)
		}
	}
	assertScoped("live", store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertScoped("reopen", reopened)
}

func searchResultsContainPath(results []SearchResult, path string) bool {
	for _, result := range results {
		if result.Path == path {
			return true
		}
	}
	return false
}

func TestFactSearchRequiresNamespaceForCollidingSubjectAlias(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	for index, subject := range []string{"project:alpha", "person:alpha"} {
		value := []string{"9350만원", "9400만원"}[index]
		if _, err := store.UpsertFact(FactInput{
			Subject: subject, Key: "quote.amount", Value: value,
			Kind: FactKindAmount, Authority: FactAuthorityPrimaryDoc,
			Sources: []string{"doc:" + subject}, BasisAt: base, At: base,
		}); err != nil {
			t.Fatal(err)
		}
	}

	bare, err := store.Search(context.Background(), "alpha 견적 금액", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range bare {
		if hit.FactID != "" {
			t.Fatalf("ambiguous bare alias exposed a fact: %+v", hit)
		}
	}

	namespaced, err := store.Search(context.Background(), "project alpha 견적 금액", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(namespaced) == 0 || namespaced[0].FactID == "" || namespaced[0].SubjectID != "project:alpha" {
		t.Fatalf("namespaced query did not select project:alpha: %+v", namespaced)
	}
	for _, hit := range namespaced {
		if hit.FactID != "" && hit.SubjectID != "project:alpha" {
			t.Fatalf("namespaced query leaked colliding subject: %+v", hit)
		}
	}
	koreanNamespaced, err := store.Search(context.Background(), "프로젝트 alpha의 견적 금액", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(koreanNamespaced) == 0 || koreanNamespaced[0].SubjectID != "project:alpha" {
		t.Fatalf("Korean namespace/particle query did not select project:alpha: %+v", koreanNamespaced)
	}
	valueOnly, err := store.Search(context.Background(), "9350만원", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range valueOnly {
		if hit.FactID != "" {
			t.Fatalf("value-only query bypassed explicit non-self subject gate: %+v", hit)
		}
	}
}

func TestSearchOmitsTombstonedFact(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	oldPage := NewPage("alpha 계약 납기", "프로젝트", nil)
	oldPage.Body = "alpha 계약 납기는 2026-09-01이었다."
	if err := store.WritePage("프로젝트/alpha-deadline.md", oldPage); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertFact(FactInput{
		Subject: "project:alpha", Key: "contract.deadline", Value: "2026-09-01",
		Kind: FactKindDeadline, Authority: FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha-contract"}, BasisAt: base, At: base,
	}); err != nil {
		t.Fatal(err)
	}
	result, err := store.TombstoneFact(FactTombstoneInput{
		Subject: "project:alpha", Key: "contract.deadline",
		Authority: FactAuthorityDirectUser, Reason: "explicit user forget", At: base.Add(time.Hour),
	})
	if err != nil || result.Status != FactStatusTombstoned {
		t.Fatalf("TombstoneFact = %+v, err=%v", result, err)
	}
	hits, err := store.Search(context.Background(), "alpha 계약 납기", 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range hits {
		if hit.FactID != "" || strings.Contains(hit.Content+hit.ExpandedContent, "2026-09-01") || hit.Path == "프로젝트/alpha-deadline.md" {
			t.Fatalf("tombstoned fact entered search: %+v", hit)
		}
	}
}

func TestFactSearchValueMatchingUsesAtomicBoundaries(t *testing.T) {
	tests := []struct {
		text  string
		value string
		want  bool
	}{
		{text: "Bob", value: "Bob", want: true},
		{text: "Bobcat", value: "Bob", want: false},
		{text: "1800", value: "800", want: false},
		{text: "금액은 800만원", value: "800", want: true},
		{text: "A는 이전 값", value: "A", want: true},
		{text: "A였어요", value: "A", want: true},
		{text: "A-1", value: "A", want: false},
		{text: "A-1", value: "1", want: false},
		{text: "상태 N/A", value: "N/A", want: true},
		{text: "상태 ✅", value: "✅", want: true},
	}
	for _, test := range tests {
		if got := factSearchValueMatch(test.text, test.value); got != test.want {
			t.Errorf("factSearchValueMatch(%q, %q) = %t, want %t", test.text, test.value, got, test.want)
		}
	}
}

func TestFactSearchDoesNotMatchSharedCompositeKeyPrefix(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	if _, err := store.UpsertFact(FactInput{
		Subject: "project:alpha", Key: "quote.amount", Value: "9350만원",
		Kind: FactKindAmount, Authority: FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha"}, BasisAt: base, At: base,
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := store.Search(context.Background(), "alpha quote owner", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range hits {
		if hit.FactID != "" {
			t.Fatalf("shared key prefix admitted the wrong fact: %+v", hit)
		}
	}
}

func TestSearchPlanWeightsFactCandidatesAndKeepsIntentRerankOnly(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	if _, err := store.UpsertFact(FactInput{
		Subject: "project:alpha", Key: "quote.amount", Value: "9350만원",
		Kind: FactKindAmount, Authority: FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha"}, BasisAt: base, At: base,
	}); err != nil {
		t.Fatal(err)
	}
	page := NewPage("원질의 설계", "프로젝트", nil)
	page.Body = "ORIGINAL-ANCHOR 원질의의 중요한 설계 문서"
	if err := store.WritePage("프로젝트/original.md", page); err != nil {
		t.Fatal(err)
	}
	report, err := store.SearchPlan(context.Background(), QueryPlan{Clauses: []QueryClause{
		{Kind: QueryKindLex, Query: "ORIGINAL-ANCHOR", Weight: 2},
		{Kind: QueryKindLex, Query: "alpha 견적 금액", Weight: 0.01},
	}}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) == 0 || report.Results[0].Path != "프로젝트/original.md" {
		t.Fatalf("low-weight expansion fact outranked original clause: %+v", report.Results)
	}
	intentOnly, err := store.SearchPlan(context.Background(), QueryPlan{
		Clauses: []QueryClause{{Kind: QueryKindLex, Query: "ORIGINAL-ANCHOR", Weight: 1}},
		Intent:  "alpha 견적 금액",
	}, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range intentOnly.Results {
		if hit.FactID != "" {
			t.Fatalf("rerank-only intent admitted a new fact: %+v", hit)
		}
	}
}

func TestFactCandidatesReserveAtLeastOnePageEvidenceSlot(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	page := NewPage("alpha dossier", "프로젝트", nil)
	page.Body = "alpha 프로젝트의 관련 회의와 배경 문서"
	if err := store.WritePage("프로젝트/alpha.md", page); err != nil {
		t.Fatal(err)
	}
	for index := range 8 {
		if _, err := store.UpsertFact(FactInput{
			Subject: "project:alpha", Key: "field." + string(rune('a'+index)), Value: "value-" + string(rune('a'+index)),
			Kind: FactKindGeneric, Authority: FactAuthorityAgent,
		}); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := store.Search(context.Background(), "alpha", 5)
	if err != nil {
		t.Fatal(err)
	}
	pageSeen := false
	for _, hit := range hits {
		pageSeen = pageSeen || hit.FactID == ""
	}
	if !pageSeen {
		t.Fatalf("fact candidates consumed every page evidence slot: %+v", hits)
	}
}

func TestExcludeFactResultsPreservesPageOnlySearchAndPlanWindows(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	for index := range 8 {
		page := NewPage("alpha page "+string(rune('a'+index)), "프로젝트", nil)
		page.Body = "alpha dossier page " + string(rune('a'+index))
		if err := store.WritePage("프로젝트/alpha-"+string(rune('a'+index))+".md", page); err != nil {
			t.Fatal(err)
		}
		if _, err := store.UpsertFact(FactInput{
			Subject: "project:alpha", Key: "field." + string(rune('a'+index)), Value: "value-" + string(rune('a'+index)),
			Kind: FactKindGeneric, Authority: FactAuthorityAgent,
		}); err != nil {
			t.Fatal(err)
		}
	}

	assertNoFacts := func(label string, results []SearchResult) {
		t.Helper()
		if len(results) != 5 {
			t.Fatalf("%s returned %d results, want five page slots: %+v", label, len(results), results)
		}
		for _, result := range results {
			if result.FactID != "" {
				t.Fatalf("%s returned synthetic fact %+v", label, result)
			}
		}
	}

	baseline, err := store.SearchWithOptions(context.Background(), "alpha", 5, QueryOptions{Mode: SearchModeBM25})
	if err != nil {
		t.Fatal(err)
	}
	assertNoFacts("bm25 baseline", baseline.Results)
	baselinePaths := make([]string, len(baseline.Results))
	for i := range baseline.Results {
		baselinePaths[i] = baseline.Results[i].Path
	}
	for _, mode := range []SearchMode{SearchModeAuto, SearchModeFull} {
		allPlane, searchErr := store.SearchWithOptions(context.Background(), "alpha", 5, QueryOptions{Mode: mode})
		if searchErr != nil {
			t.Fatal(searchErr)
		}
		factSeen := false
		for _, result := range allPlane.Results {
			factSeen = factSeen || result.FactID != ""
		}
		if !factSeen {
			t.Fatalf("%s all-plane search did not expose a fact fixture: %+v", mode, allPlane.Results)
		}

		pagePlane, searchErr := store.SearchWithOptions(context.Background(), "alpha", 5, QueryOptions{
			Mode: mode, ExcludeFactResults: true,
		})
		if searchErr != nil {
			t.Fatal(searchErr)
		}
		assertNoFacts(string(mode), pagePlane.Results)
		for i, result := range pagePlane.Results {
			if result.Path != baselinePaths[i] {
				t.Fatalf("%s page rank %d = %q, want baseline %q", mode, i, result.Path, baselinePaths[i])
			}
		}
	}

	plan := QueryPlan{Clauses: []QueryClause{{Kind: QueryKindLex, Query: "alpha"}}}
	allPlan, err := store.SearchPlan(context.Background(), plan, 5)
	if err != nil {
		t.Fatal(err)
	}
	factSeen := false
	for _, result := range allPlan.Results {
		factSeen = factSeen || result.FactID != ""
	}
	if !factSeen {
		t.Fatalf("all-plane plan did not expose a fact fixture: %+v", allPlan.Results)
	}
	pagePlan, err := store.SearchPlanWithOptions(context.Background(), plan, 5, QueryOptions{ExcludeFactResults: true})
	if err != nil {
		t.Fatal(err)
	}
	assertNoFacts("typed plan", pagePlan.Results)
}

func TestSyntheticFactReadIsExtensionOptionalAndNamespaceReserved(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	created, err := store.UpsertFact(FactInput{
		Subject: "self", Key: "communication.response_length", Value: "짧게",
		Kind: FactKindPreference, Authority: FactAuthorityDirectUser,
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := factSearchPathPrefix + created.ClaimID
	page, err := store.ReadPage(ref)
	if err != nil || !strings.Contains(page.Body, "짧게") {
		t.Fatalf("ReadPage(%q) = %+v, %v", ref, page, err)
	}
	if err := store.WritePage(ref, NewPage("hijack", "프로젝트", nil)); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("synthetic namespace write error = %v", err)
	}
}

func TestSyntheticFactNamespaceRejectsEveryPageMutation(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	path := "@facts/fm-hijack.md"
	operations := []struct {
		name string
		run  func() error
	}{
		{name: "write", run: func() error { return store.WritePage(path, NewPage("bad", "프로젝트", nil)) }},
		{name: "update", run: func() error {
			return store.UpdatePage(path, func(page *Page) (*Page, error) { return page, nil })
		}},
		{name: "delete", run: func() error { return store.DeletePage(path) }},
		{name: "move-from", run: func() error { return store.MovePage(path, "프로젝트/other.md") }},
		{name: "move-to", run: func() error { return store.MovePage("프로젝트/other.md", path) }},
		{name: "mark", run: func() error { return store.MarkSuperseded(path, "프로젝트/other.md") }},
		{name: "merge", run: func() error {
			_, err := store.MergePage(path, "프로젝트/other.md", "bad", MergeOptions{})
			return err
		}},
		{name: "forget", run: func() error {
			_, err := store.Forget(path, "bad")
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.run(); err == nil || !strings.Contains(err.Error(), "reserved fact-journal projection") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestFactSearchInjectionIsProductionModeOnlyAndProjectionIsNotDuplicated(t *testing.T) {
	root := t.TempDir()
	wikiDir, diaryDir := filepath.Join(root, "wiki"), filepath.Join(root, "diary")
	store, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertFact(FactInput{
		Subject: "self", Key: "communication.response_length", Value: "짧게 답변",
		Kind: FactKindPreference, Authority: FactAuthorityDirectUser,
	}); err != nil {
		t.Fatal(err)
	}
	assertModes := func(label string, current *Store) {
		t.Helper()
		auto, err := current.SearchWithOptions(context.Background(), "response length", 10, QueryOptions{Mode: SearchModeAuto})
		if err != nil {
			t.Fatal(err)
		}
		factCount := 0
		for _, hit := range auto.Results {
			if hit.Path == factProfilePagePath {
				t.Fatalf("%s returned generated compatibility projection: %+v", label, hit)
			}
			if hit.FactID != "" {
				factCount++
			}
		}
		if factCount != 1 {
			t.Fatalf("%s auto fact count = %d, results=%+v", label, factCount, auto.Results)
		}
		bm25, err := current.SearchWithOptions(context.Background(), "response length", 10, QueryOptions{Mode: SearchModeBM25})
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range bm25.Results {
			if hit.FactID != "" || hit.Path == factProfilePagePath {
				t.Fatalf("%s BM25 stage was contaminated by fact projection: %+v", label, hit)
			}
		}
	}
	assertModes("live", store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertModes("restart", reopened)
}

func TestSemanticStatusUsesSameFactAndSupersessionCorpusAsRefresh(t *testing.T) {
	root := t.TempDir()
	wikiDir, diaryDir := filepath.Join(root, "wiki"), filepath.Join(root, "diary")
	store, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	current := NewPage("현재 프로젝트", "프로젝트", nil)
	current.Body = "현재 프로젝트의 GPU 위험 평가와 납기 차질 검토"
	if err := store.WritePage("프로젝트/current.md", current); err != nil {
		t.Fatal(err)
	}
	old := NewPage("이전 프로젝트", "프로젝트", nil)
	old.Body = "이전 프로젝트의 GPU 위험 평가와 납기 차질 검토"
	if err := store.WritePage("프로젝트/old.md", old); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSuperseded("프로젝트/old.md", "프로젝트/current.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertFact(FactInput{
		Subject: "self", Key: "communication.response_length", Value: "짧게 답변",
		Kind: FactKindPreference, Authority: FactAuthorityDirectUser,
	}); err != nil {
		t.Fatal(err)
	}
	store.SetEmbedder(fakeEmbedder{healthy: true, fingerprint: "fact-search:2", dimensions: 2})
	if err := store.WarmSemanticIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.SemanticStatus()
	if !status.Healthy || status.Expected != 1 || status.Indexed != 1 || status.Pending != 0 || status.Stale != 0 {
		t.Fatalf("live semantic status counted excluded pages: %+v", status)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopened.SetEmbedder(fakeEmbedder{healthy: true, fingerprint: "fact-search:2", dimensions: 2})
	status = reopened.SemanticStatus()
	if !status.Healthy || status.Expected != 1 || status.Indexed != 1 || status.Pending != 0 || status.Stale != 0 {
		t.Fatalf("restart semantic status counted excluded pages: %+v", status)
	}
}

func TestManualFactProfileRemainsInCorpusUntilGeneratedCutover(t *testing.T) {
	root := t.TempDir()
	wikiDir, diaryDir := filepath.Join(root, "wiki"), filepath.Join(root, "diary")
	path := filepath.Join(wikiDir, filepath.FromSlash(factProfilePagePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	manual := NewPage("수동 현행 사실", "사용자", nil)
	manual.Meta.Importance = 0.99
	manual.Body = "MANUAL-PROFILE-ANCHOR 운영자가 직접 관리하는 기존 페이지"
	if err := os.WriteFile(path, manual.Render(), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hits, err := store.SearchWithOptions(context.Background(), "MANUAL-PROFILE-ANCHOR", 5, QueryOptions{Mode: SearchModeBM25})
	if err != nil || len(hits.Results) == 0 || hits.Results[0].Path != factProfilePagePath {
		t.Fatalf("manual profile was hidden before cutover: %+v, err=%v", hits.Results, err)
	}
	tier1 := store.Tier1Pages(0.9)
	if len(tier1) != 1 || tier1[0].Path != factProfilePagePath {
		t.Fatalf("manual profile was removed from Tier1 before cutover: %+v", tier1)
	}
	store.SetEmbedder(fakeEmbedder{healthy: true, fingerprint: "manual-profile:2", dimensions: 2})
	if err := store.WarmSemanticIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.SemanticStatus()
	if !status.Healthy || status.Expected != 1 || status.Indexed != 1 {
		t.Fatalf("manual profile was removed from semantic corpus before cutover: %+v", status)
	}
}
