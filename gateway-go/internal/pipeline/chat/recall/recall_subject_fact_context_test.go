package recall

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

func TestSubjectAwareCurrentFactsPromotesMatchedWinnerAheadOfSelfBudget(t *testing.T) {
	store := newSubjectFactRecallStore(t)
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	const (
		oldAmount = "8120만원"
		newAmount = "9350만원"
		selfValue = "답변은 짧고 간결하게 한다"
	)
	if err := store.AppendDiary("alpha 견적 금액은 " + oldAmount + "이었다."); err != nil {
		t.Fatal(err)
	}
	putSubjectFact(t, store, wiki.FactInput{
		Subject: "project:alpha", Key: "quote.amount", Value: oldAmount,
		Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha-quote-v1"}, BasisAt: base, At: base,
	})
	putSubjectFact(t, store, wiki.FactInput{
		Subject: "project:alpha", Key: "quote.amount", Value: newAmount,
		Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha-quote-v2"}, BasisAt: base.Add(time.Hour), At: base.Add(time.Hour),
	})
	putSubjectFact(t, store, wiki.FactInput{
		Subject: "self", Key: "communication.response_length", Value: selfValue,
		Kind: wiki.FactKindPreference, Authority: wiki.FactAuthorityDirectUser, At: base,
	})
	// Force truncation pressure: the matched commercial winner must be rendered
	// before the many self preferences, not disappear at the tail of the budget.
	for i := 0; i < 14; i++ {
		putSubjectFact(t, store, wiki.FactInput{
			Subject: "self", Key: fmt.Sprintf("preference.long.%02d", i),
			Value: strings.Repeat(fmt.Sprintf("개인선호%d", i), 35),
			Kind:  wiki.FactKindPreference, Authority: wiki.FactAuthorityDirectUser,
			At: base.Add(time.Duration(i+2) * time.Hour),
		})
	}

	out := buildSubjectFactRecall(t, store, "alpha 견적 금액 기억나?")
	if !strings.Contains(out, "[project:alpha]") || !strings.Contains(out, newAmount) {
		t.Fatalf("matched non-self winner missing:\n%s", out)
	}
	if strings.Contains(out, oldAmount) {
		t.Fatalf("superseded amount leaked from diary/history:\n%s", out)
	}
	if !strings.Contains(out, selfValue) {
		t.Fatalf("self facts were not retained with matched subject:\n%s", out)
	}
	if projectAt, selfAt := strings.Index(out, newAmount), strings.Index(out, selfValue); projectAt < 0 || selfAt < 0 || projectAt >= selfAt {
		t.Fatalf("matched high-risk fact was not budget-prioritized: project=%d self=%d\n%s", projectAt, selfAt, out)
	}
}

func TestSubjectAwareCurrentFactsForgetDoesNotLeakFromDiary(t *testing.T) {
	store := newSubjectFactRecallStore(t)
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	const forgotten = "2026-09-01"
	if err := store.AppendDiary("alpha 계약 납기일은 " + forgotten + "이다."); err != nil {
		t.Fatal(err)
	}
	putSubjectFact(t, store, wiki.FactInput{
		Subject: "project:alpha", Key: "contract.deadline", Value: forgotten,
		Kind: wiki.FactKindDeadline, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha-contract"}, BasisAt: base, At: base,
	})
	result, err := store.TombstoneFact(wiki.FactTombstoneInput{
		Subject: "project:alpha", Key: "contract.deadline",
		Authority: wiki.FactAuthorityAgent, Reason: "contract cancelled", At: base.Add(time.Hour),
	})
	if err != nil || !result.Committed || result.Status != wiki.FactStatusTombstoned {
		t.Fatalf("TombstoneFact = %+v, err=%v", result, err)
	}

	out := buildSubjectFactRecall(t, store, "alpha 계약 납기 기억나?")
	if strings.Contains(out, forgotten) {
		t.Fatalf("forgotten non-self fact leaked:\n%s", out)
	}
}

func TestSubjectAwareCurrentFactsShortCorrectionBlocksAttachedDiaryValue(t *testing.T) {
	store := newSubjectFactRecallStore(t)
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	if err := store.AppendDiary("alpha release status는 A였다"); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendDiary("alpha project remains active KEEP-ME"); err != nil {
		t.Fatal(err)
	}
	for index, value := range []string{"A", "B"} {
		putSubjectFact(t, store, wiki.FactInput{
			Subject: "project:alpha", Key: "release.status", Value: value,
			Kind: wiki.FactKindSystemState, Authority: wiki.FactAuthorityRuntime,
			Sources: []string{"runtime:alpha"}, At: base.Add(time.Duration(index) * time.Hour),
		})
	}

	out := buildSubjectFactRecall(t, store, "alpha release status 기억나?")
	if !strings.Contains(out, "): B") {
		t.Fatalf("short current winner missing:\n%s", out)
	}
	if strings.Contains(out, "A였다") {
		t.Fatalf("short stale value with Korean particle leaked:\n%s", out)
	}
	if strings.Count(out, "): B") != 1 {
		t.Fatalf("canonical fact was duplicated into untrusted recall evidence:\n%s", out)
	}
}

func TestSubjectAwareCurrentFactsDoNotEmitNoEvidenceForFactOnlyCue(t *testing.T) {
	store := newSubjectFactRecallStore(t)
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	const currentAmount = "9350만원"
	putSubjectFact(t, store, wiki.FactInput{
		Subject: "project:alpha", Key: "quote.amount", Value: currentAmount,
		Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha-quote-v2"}, BasisAt: base, At: base,
	})

	out := buildSubjectFactRecall(t, store, "alpha 견적 금액 기억나?")
	if !strings.Contains(out, currentAmount) {
		t.Fatalf("fact-only cue omitted current winner:\n%s", out)
	}
	if strings.Contains(out, "source=none") || strings.Contains(out, "근거를 찾지 못했다") {
		t.Fatalf("fact-only cue emitted contradictory no-evidence notice:\n%s", out)
	}

	unrelated := buildSubjectFactRecall(t, store, "alpha 지난 회의 참석자 기억나?")
	if !strings.Contains(unrelated, "source=none") || !strings.Contains(unrelated, "근거를 찾지 못했다") {
		t.Fatalf("unrelated subject fact incorrectly satisfied a specific cue:\n%s", unrelated)
	}
	if strings.Contains(unrelated, currentAmount) || strings.Contains(unrelated, "[project:alpha]") {
		t.Fatalf("unrelated aspect injected an ambient project amount:\n%s", unrelated)
	}
}

func TestSubjectAwareCurrentFactsRejectShortAndCompetingAspectCues(t *testing.T) {
	store := newSubjectFactRecallStore(t)
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	const currentAmount = "9350만원"
	putSubjectFact(t, store, wiki.FactInput{
		Subject: "project:alpha", Key: "quote.amount", Value: currentAmount,
		Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha-quote-v2"}, BasisAt: base, At: base,
	})

	for _, message := range []string{
		"alpha PM 기억나?",
		"alpha quote owner 기억나?",
	} {
		t.Run(message, func(t *testing.T) {
			out := buildSubjectFactRecall(t, store, message)
			if !strings.Contains(out, "source=none") || !strings.Contains(out, "근거를 찾지 못했다") {
				t.Fatalf("specific unmatched cue was treated as resolved:\n%s", out)
			}
			if strings.Contains(out, currentAmount) || strings.Contains(out, "[project:alpha]") {
				t.Fatalf("unrelated amount satisfied %q:\n%s", message, out)
			}
		})
	}

	positive := buildSubjectFactRecall(t, store, "alpha quote amount 기억나?")
	if !strings.Contains(positive, currentAmount) || strings.Contains(positive, "source=none") {
		t.Fatalf("matching composite aspect did not resolve current amount:\n%s", positive)
	}
}

func TestSubjectAwareCurrentFactsMatchesKoreanGenitiveSubject(t *testing.T) {
	store := newSubjectFactRecallStore(t)
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	const currentAmount = "9350만원"
	putSubjectFact(t, store, wiki.FactInput{
		Subject: "project:alpha", Key: "quote.amount", Value: currentAmount,
		Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha-quote-v2"}, BasisAt: base, At: base,
	})

	out := buildSubjectFactRecall(t, store, "alpha의 견적 금액 기억나?")
	if !strings.Contains(out, "[project:alpha]") || !strings.Contains(out, currentAmount) {
		t.Fatalf("Korean genitive subject did not expose its current fact:\n%s", out)
	}
	if strings.Contains(out, "source=none") {
		t.Fatalf("Korean genitive fact cue emitted no-evidence:\n%s", out)
	}
}

func TestVagueCueWithOnlyAmbientSelfFactsStillReportsNoEvidence(t *testing.T) {
	store := newSubjectFactRecallStore(t)
	putSubjectFact(t, store, wiki.FactInput{
		Subject: "self", Key: "communication.response_length", Value: "짧고 간결하게 답한다",
		Kind: wiki.FactKindPreference, Authority: wiki.FactAuthorityDirectUser,
		At: time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC),
	})

	out := buildSubjectFactRecall(t, store, "그거 기억나?")
	if !strings.Contains(out, "source=none") || !strings.Contains(out, "근거를 찾지 못했다") {
		t.Fatalf("ambient self facts incorrectly satisfied a vague recall cue:\n%s", out)
	}
}

func TestSubjectAwareSelfFactsRenderSuppliedRecallEpoch(t *testing.T) {
	const value = "짧고 간결하게 답한다"
	out := subjectAwareCurrentFactContext(
		42,
		[]wiki.FactClaim{{
			Subject: "self", Key: "communication.response_length", Value: value,
			Kind: wiki.FactKindPreference, Authority: wiki.FactAuthorityDirectUser,
			Status: wiki.FactStatusCurrent, Revision: 42,
		}},
		nil,
		"그거 기억나?",
		currentFactMaxChars,
	)
	if !strings.Contains(out, `revision="42"`) || !strings.Contains(out, value) {
		t.Fatalf("self context did not render supplied fact epoch:\n%s", out)
	}
}

func TestSubjectAwareCurrentFactsIsolatesUnrelatedAndVagueSubjects(t *testing.T) {
	store := newSubjectFactRecallStore(t)
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	const (
		alphaValue = "ALPHA-WINNER-9350"
		betaCode   = "BETA-CODE-771"
	)
	putSubjectFact(t, store, wiki.FactInput{
		Subject: "project:alpha", Key: "quote.amount", Value: alphaValue,
		Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha"}, BasisAt: base, At: base,
	})
	putSubjectFact(t, store, wiki.FactInput{
		Subject: "project:beta", Key: "contract.code", Value: betaCode,
		Kind: wiki.FactKindContract, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"doc:beta"}, BasisAt: base, At: base,
	})
	// This row lexically matches alpha but contains beta's current value. Since a
	// diary row has no reliable subject ID, it must be removed rather than leak.
	if err := store.AppendDiary("alpha 비교 메모에 beta 계약 코드 " + betaCode + "도 적혀 있다."); err != nil {
		t.Fatal(err)
	}
	const betaParaphrase = "BETA-PARAPHRASE-SHOULD-NOT-LEAK"
	if err := store.AppendDiary("alpha 비교 메모: beta의 계약 조건이 새 형식으로 바뀌었다. " + betaParaphrase); err != nil {
		t.Fatal(err)
	}

	alphaOut := buildSubjectFactRecall(t, store, "alpha 견적 확인해줘")
	if !strings.Contains(alphaOut, alphaValue) {
		t.Fatalf("explicit alpha did not expose its current fact:\n%s", alphaOut)
	}
	if strings.Contains(alphaOut, betaCode) || strings.Contains(alphaOut, betaParaphrase) || strings.Contains(alphaOut, "[project:beta]") {
		t.Fatalf("unrelated beta subject leaked into alpha turn:\n%s", alphaOut)
	}

	unrelatedOut := buildSubjectFactRecall(t, store, "gamma 프로젝트 기억나?")
	if strings.Contains(unrelatedOut, alphaValue) || strings.Contains(unrelatedOut, betaCode) {
		t.Fatalf("unmentioned subjects leaked into unrelated query:\n%s", unrelatedOut)
	}

	vagueOut := buildSubjectFactRecall(t, store, "그거 기억나?")
	if strings.Contains(vagueOut, alphaValue) || strings.Contains(vagueOut, betaCode) || strings.Contains(vagueOut, betaParaphrase) ||
		strings.Contains(vagueOut, "[project:alpha]") || strings.Contains(vagueOut, "[project:beta]") {
		t.Fatalf("vague cue leaked non-self facts:\n%s", vagueOut)
	}
}

func TestExplicitFactSubjectDisambiguatesNamespaceCollision(t *testing.T) {
	claims := []wiki.FactClaim{
		{Subject: "project:alpha", Value: "project-value", Status: wiki.FactStatusCurrent},
		{Subject: "person:alpha", Value: "person-value", Status: wiki.FactStatusCurrent},
	}
	if matched := explicitlyMatchedFactSubjects("alpha 기억나?", claims); len(matched) != 0 {
		t.Fatalf("ambiguous bare identity matched subjects: %#v", matched)
	}
	matched := explicitlyMatchedFactSubjects("project alpha 기억나?", claims)
	if len(matched) != 1 {
		t.Fatalf("namespaced identity matches = %#v", matched)
	}
	if _, ok := matched["project:alpha"]; !ok {
		t.Fatalf("project subject was not selected: %#v", matched)
	}
}

func TestExplicitFactSubjectSplitsSlugSeparators(t *testing.T) {
	claims := []wiki.FactClaim{
		{Subject: "person:john-smith", Value: "person-value", Status: wiki.FactStatusCurrent},
		{Subject: "project:alpha_beta", Value: "project-value", Status: wiki.FactStatusCurrent},
		{Subject: "project:a-1", Value: "code-value", Status: wiki.FactStatusCurrent},
	}
	for message, want := range map[string]string{
		"John Smith 계약 기억나?": "person:john-smith",
		"alpha beta 상태 기억나?": "project:alpha_beta",
		"A-1 상태 기억나?":        "project:a-1",
	} {
		matched := explicitlyMatchedFactSubjects(message, claims)
		if len(matched) != 1 {
			t.Fatalf("explicitlyMatchedFactSubjects(%q) = %#v", message, matched)
		}
		if _, ok := matched[want]; !ok {
			t.Fatalf("explicitlyMatchedFactSubjects(%q) missed %q: %#v", message, want, matched)
		}
	}
}

func TestUnmatchedSlugSubjectAliasDoesNotLeakParaphrase(t *testing.T) {
	store := newSubjectFactRecallStore(t)
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	putSubjectFact(t, store, wiki.FactInput{
		Subject: "project:alpha", Key: "quote.amount", Value: "ALPHA-AMOUNT-9350",
		Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha"}, BasisAt: base, At: base,
	})
	putSubjectFact(t, store, wiki.FactInput{
		Subject: "person:john-smith", Key: "contract.role", Value: "PRIVATE-ROLE-771",
		Kind: wiki.FactKindContract, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"doc:john-smith"}, BasisAt: base, At: base,
	})
	const paraphrase = "SLUG-PARAPHRASE-SHOULD-NOT-LEAK"
	if err := store.AppendDiary("alpha 비교 메모: John Smith 계약 조건이 바뀌었다. " + paraphrase); err != nil {
		t.Fatal(err)
	}

	out := buildSubjectFactRecall(t, store, "alpha 견적 확인해줘")
	if strings.Contains(out, paraphrase) || strings.Contains(out, "John Smith") || strings.Contains(out, "[person:john-smith]") {
		t.Fatalf("unmatched slug subject paraphrase leaked:\n%s", out)
	}

	johnOut := buildSubjectFactRecall(t, store, "John Smith 계약 role 기억나?")
	if !strings.Contains(johnOut, "[person:john-smith]") || !strings.Contains(johnOut, "PRIVATE-ROLE-771") {
		t.Fatalf("explicit natural-language slug subject did not expose its fact:\n%s", johnOut)
	}
	if strings.Contains(johnOut, "ALPHA-AMOUNT-9350") || strings.Contains(johnOut, "[project:alpha]") {
		t.Fatalf("unmatched alpha subject leaked into John Smith turn:\n%s", johnOut)
	}
}

func TestRenderSubjectFactContextNeverCutsClaimLine(t *testing.T) {
	value := "9350" + strings.Repeat("만원", 120)
	claim := wiki.FactClaim{
		Subject: "project:alpha", Key: "quote.amount", Value: value,
		Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
		Status: wiki.FactStatusCurrent,
	}
	out := renderSubjectFactContext(7, []wiki.FactClaim{claim}, 360)
	if !strings.Contains(out, "현행 사실 일부 생략") || !strings.HasSuffix(out, "</current-facts>") {
		t.Fatalf("bounded fact context malformed:\n%s", out)
	}
	if strings.Contains(out, "9350") || strings.Contains(out, string([]rune(value)[:20])) {
		t.Fatalf("fact claim was injected as a truncated partial line:\n%s", out)
	}
}

func newSubjectFactRecallStore(t *testing.T) *wiki.Store {
	t.Helper()
	root := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(root, "wiki"), filepath.Join(root, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func putSubjectFact(t *testing.T, store *wiki.Store, input wiki.FactInput) {
	t.Helper()
	result, err := store.UpsertFact(input)
	if err != nil || !result.Committed {
		t.Fatalf("UpsertFact(%s/%s) = %+v, err=%v", input.Subject, input.Key, result, err)
	}
}

func buildSubjectFactRecall(t *testing.T, store *wiki.Store, message string) string {
	t.Helper()
	out, truncated := Build(
		context.Background(),
		Params{SessionKey: "client:main:subject-facts", Message: message},
		Deps{Wiki: store},
		nil,
	)
	if truncated {
		t.Fatalf("Build(%q) unexpectedly truncated", message)
	}
	return out
}
