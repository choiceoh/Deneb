package recall

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

func TestRecallFactLifecycleCorrectionRemovesOldDiaryValueAcrossReopen(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")
	diaryDir := filepath.Join(root, "diary")
	store := openRecallFactLifecycleStore(t, wikiDir, diaryDir)

	const (
		oldValue = "오로라 회의 기본 시간은 밤 9시다"
		newValue = "오로라 회의 기본 시간은 오전 9시다"
	)
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	if err := store.AppendDiary(oldValue + "."); err != nil {
		t.Fatalf("AppendDiary: %v", err)
	}
	upsertRecallFact(t, store, wiki.FactInput{
		Subject: "self", Key: "schedule.aurora_meeting", Value: oldValue,
		Kind: wiki.FactKindPreference, Authority: wiki.FactAuthorityDirectUser,
		Sources: []string{"session:client:main#1"}, At: base,
	})

	before := buildRecallFactLifecycle(t, store, "오로라 회의 시간 기억나?")
	if !strings.Contains(before, oldValue) {
		t.Fatalf("precondition: old diary value was not recallable before correction:\n%s", before)
	}

	upsertRecallFact(t, store, wiki.FactInput{
		Subject: "self", Key: "schedule.aurora_meeting", Value: newValue,
		Kind: wiki.FactKindPreference, Authority: wiki.FactAuthorityDirectUser,
		Sources: []string{"session:client:main#2"}, At: base.Add(time.Hour),
	})
	assertRecallFactLifecycle(t, store, "direct correction", "오로라 회의 시간 기억나?", newValue, oldValue)
	assertRecallFactLifecycle(t, store, "vague correction", "그거 기억나?", newValue, oldValue)

	if err := store.Close(); err != nil {
		t.Fatalf("Close before reopen: %v", err)
	}
	reopened := openRecallFactLifecycleStore(t, wikiDir, diaryDir)
	t.Cleanup(func() { _ = reopened.Close() })
	assertRecallFactLifecycle(t, reopened, "direct after reopen", "오로라 회의 시간 기억나?", newValue, oldValue)
	assertRecallFactLifecycle(t, reopened, "vague after reopen", "그거 기억나?", newValue, oldValue)
}

func TestRecallFactLifecycleForgetRemovesTombstonedDiaryValue(t *testing.T) {
	root := t.TempDir()
	store := openRecallFactLifecycleStore(t, filepath.Join(root, "wiki"), filepath.Join(root, "diary"))
	t.Cleanup(func() { _ = store.Close() })

	const forgotten = "네뷸라 집중 시간은 매일 오후 4시다"
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	if err := store.AppendDiary(forgotten + "."); err != nil {
		t.Fatalf("AppendDiary: %v", err)
	}
	upsertRecallFact(t, store, wiki.FactInput{
		Subject: "self", Key: "schedule.nebula_focus", Value: forgotten,
		Kind: wiki.FactKindPreference, Authority: wiki.FactAuthorityDirectUser,
		Sources: []string{"session:client:main#3"}, At: base,
	})
	result, err := store.TombstoneFact(wiki.FactTombstoneInput{
		Subject: "self", Key: "schedule.nebula_focus",
		Authority: wiki.FactAuthorityDirectUser,
		Sources:   []string{"session:client:main#4"}, Reason: "user requested forget",
		At: base.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("TombstoneFact: %v", err)
	}
	if !result.Committed || result.Status != wiki.FactStatusTombstoned {
		t.Fatalf("TombstoneFact result=%+v", result)
	}

	assertRecallFactLifecycle(t, store, "direct forget", "네뷸라 집중 시간 기억나?", "", forgotten)
	assertRecallFactLifecycle(t, store, "vague forget", "그거 기억나?", "", forgotten)
}

func TestRecallFactLifecycleKeepsUnrelatedSharedShortDiaryValueAcrossReopen(t *testing.T) {
	root := t.TempDir()
	wikiDir, diaryDir := filepath.Join(root, "wiki"), filepath.Join(root, "diary")
	store := openRecallFactLifecycleStore(t, wikiDir, diaryDir)
	const unrelated = "품질 등급 A는 통과"
	if err := store.AppendDiary(unrelated + "."); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	for index, value := range []string{"A", "B"} {
		upsertRecallFact(t, store, wiki.FactInput{
			Subject: "self", Key: "preference.language", Value: value,
			Kind: wiki.FactKindPreference, Authority: wiki.FactAuthorityDirectUser,
			At: base.Add(time.Duration(index) * time.Hour),
		})
	}
	assertUnrelated := func(label string, current *wiki.Store) {
		t.Helper()
		out := buildRecallFactLifecycle(t, current, "품질 등급 기억나?")
		if !strings.Contains(out, unrelated) {
			t.Fatalf("%s unrelated shared A diary evidence was removed:\n%s", label, out)
		}
	}
	assertUnrelated("live", store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openRecallFactLifecycleStore(t, wikiDir, diaryDir)
	defer reopened.Close()
	assertUnrelated("reopen", reopened)
}

func TestRecallPreflightScopesSharedShortValuesAcrossFilesAndTombstone(t *testing.T) {
	root := t.TempDir()
	store := openRecallFactLifecycleStore(t, filepath.Join(root, "wiki"), filepath.Join(root, "diary"))
	defer store.Close()
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	upsertRecallFact(t, store, wiki.FactInput{
		Subject: "project:alpha", Key: "quote.amount", Value: "A",
		Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha"}, At: base, BasisAt: base,
	})
	for index, value := range []string{"A", "B"} {
		at := base.Add(time.Duration(index) * time.Hour)
		upsertRecallFact(t, store, wiki.FactInput{
			Subject: "project:beta", Key: "quote.amount", Value: value,
			Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
			Sources: []string{"doc:beta"}, At: at, BasisAt: at,
		})
	}
	files := FileRecallFunc(func(_ context.Context, query string, _ int) []FileRecallHit {
		switch {
		case strings.Contains(query, "alpha"):
			return []FileRecallHit{{Path: "/project/alpha/quote.txt", Snippet: "project alpha quote amount A", Score: 0.99}}
		case strings.Contains(query, "beta"):
			return []FileRecallHit{
				{Path: "/project/beta/quote-old.txt", Snippet: "project beta quote amount A OLD-BETA", Score: 0.99},
				{Path: "/quality/grade.txt", Snippet: "quality grade A passes KEEP-A", Score: 0.98},
			}
		default:
			return nil
		}
	})
	build := func(message string) string {
		t.Helper()
		out, truncated := Build(
			context.Background(),
			Params{SessionKey: "client:main:fact-short-files", Message: message},
			Deps{Wiki: store, FileRecall: files}, nil,
		)
		if truncated {
			t.Fatalf("Build(%q) truncated", message)
		}
		return out
	}
	alpha := build("project alpha quote amount 기억나?")
	if !strings.Contains(alpha, "project alpha quote amount A") {
		t.Fatalf("alpha current A file evidence was removed:\n%s", alpha)
	}
	beta := build("project beta quote amount 기억나?")
	if strings.Contains(beta, "OLD-BETA") || !strings.Contains(beta, "): B") || !strings.Contains(beta, "KEEP-A") {
		t.Fatalf("beta correction or unrelated shared A mismatch:\n%s", beta)
	}
	if _, err := store.TombstoneFact(wiki.FactTombstoneInput{
		Subject: "project:beta", Key: "quote.amount",
		Authority: wiki.FactAuthorityAgent, At: base.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	forgotten := build("project beta quote amount 기억나?")
	if strings.Contains(forgotten, "OLD-BETA") || strings.Contains(forgotten, "): B") || !strings.Contains(forgotten, "KEEP-A") {
		t.Fatalf("beta tombstone or unrelated shared A mismatch:\n%s", forgotten)
	}
}

func TestRecallFactLifecycleCrossSubjectCorrectionRemovesOldDiaryValue(t *testing.T) {
	root := t.TempDir()
	store := openRecallFactLifecycleStore(t, filepath.Join(root, "wiki"), filepath.Join(root, "diary"))
	t.Cleanup(func() { _ = store.Close() })

	const (
		oldValue = "머큐리 견적 금액은 8120만원이다"
		newValue = "머큐리 견적 금액은 9350만원이다"
	)
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	if err := store.AppendDiary(oldValue + "."); err != nil {
		t.Fatalf("AppendDiary: %v", err)
	}
	upsertRecallFact(t, store, wiki.FactInput{
		Subject: "project:mercury", Key: "quote.amount", Value: oldValue,
		Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"project:mercury/quote-v1"}, At: base, BasisAt: base,
	})
	upsertRecallFact(t, store, wiki.FactInput{
		Subject: "project:mercury", Key: "quote.amount", Value: newValue,
		Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"project:mercury/quote-v2"}, At: base.Add(time.Hour), BasisAt: base.Add(time.Hour),
	})

	history := store.FactHistory("project:mercury", "quote.amount")
	if len(history) != 2 || history[0].Status != wiki.FactStatusSuperseded || history[1].Status != wiki.FactStatusCurrent {
		t.Fatalf("cross-subject correction history=%+v", history)
	}
	assertRecallFactLifecycle(t, store, "named cross-subject correction", "머큐리 견적 금액 기억나?", "", oldValue)
	assertRecallFactLifecycle(t, store, "vague cross-subject correction", "그거 기억나?", "", oldValue)
}

func TestRecallSupersededLegacyPageBlocksVagueDiaryFallbackAcrossReopen(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")
	diaryDir := filepath.Join(root, "diary")
	store := openRecallFactLifecycleStore(t, wikiDir, diaryDir)

	const staleValue = "오로라 운영 포트는 18789를 사용한다"
	oldPage := wiki.NewPage("오로라 운영 포트 구버전", "시스템", nil)
	oldPage.Body = "- " + staleValue
	currentPage := wiki.NewPage("오로라 운영 포트", "시스템", nil)
	currentPage.Body = "오로라 운영 포트는 19000을 사용한다"
	if err := store.WritePage("시스템/aurora-port-old.md", oldPage); err != nil {
		t.Fatal(err)
	}
	if err := store.WritePage("시스템/aurora-port.md", currentPage); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendDiary(staleValue + "."); err != nil {
		t.Fatal(err)
	}
	if before := buildRecallFactLifecycle(t, store, "그거 기억나?"); !strings.Contains(before, staleValue) {
		t.Fatalf("precondition: vague diary fallback did not contain stale value:\n%s", before)
	}
	if err := store.MarkSuperseded("시스템/aurora-port-old.md", "시스템/aurora-port.md"); err != nil {
		t.Fatal(err)
	}
	assertRecallFactLifecycle(t, store, "vague legacy supersession", "그거 기억나?", "", staleValue)

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openRecallFactLifecycleStore(t, wikiDir, diaryDir)
	t.Cleanup(func() { _ = reopened.Close() })
	assertRecallFactLifecycle(t, reopened, "vague legacy supersession after reopen", "그거 기억나?", "", staleValue)
}

func TestRecallLegacyMigrationCorrectionBlocksOldVagueAcrossReopen(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")
	diaryDir := filepath.Join(root, "diary")
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	const (
		oldValue = "앞으로 답변은 아주 길고 상세하게 해줘"
		newValue = "정정할게. 앞으로 답변은 짧고 간결하게 해줘"
	)
	if err := os.WriteFile(filepath.Join(workspace, "MEMORY.md"), []byte("# MEMORY\n\n- "+oldValue+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "USER.md"), []byte("# USER\n\n- "+newValue+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := openRecallFactLifecycleStore(t, wikiDir, diaryDir)
	imported, err := store.ImportLegacyFactFiles(workspace)
	if err != nil || imported != 2 {
		t.Fatalf("legacy import=%d err=%v", imported, err)
	}
	if err := store.AppendDiary(oldValue + "."); err != nil {
		t.Fatal(err)
	}
	if err := store.SetFactProjectionDir(workspace); err != nil {
		t.Fatal(err)
	}
	assertRecallFactLifecycle(t, store, "legacy migration vague", "그거 기억나?", newValue, oldValue)

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openRecallFactLifecycleStore(t, wikiDir, diaryDir)
	t.Cleanup(func() { _ = reopened.Close() })
	assertRecallFactLifecycle(t, reopened, "legacy migration vague after reopen", "그거 기억나?", newValue, oldValue)
}

func openRecallFactLifecycleStore(t *testing.T, wikiDir, diaryDir string) *wiki.Store {
	t.Helper()
	store, err := wiki.NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func upsertRecallFact(t *testing.T, store *wiki.Store, input wiki.FactInput) {
	t.Helper()
	result, err := store.UpsertFact(input)
	if err != nil {
		t.Fatalf("UpsertFact(%s/%s): %v", input.Subject, input.Key, err)
	}
	if !result.Committed {
		t.Fatalf("UpsertFact(%s/%s) did not commit: %+v", input.Subject, input.Key, result)
	}
}

func buildRecallFactLifecycle(t *testing.T, store *wiki.Store, message string) string {
	t.Helper()
	out, truncated := Build(
		context.Background(),
		Params{SessionKey: "client:main:fact-lifecycle", Message: message},
		Deps{Wiki: store},
		nil,
	)
	if truncated {
		t.Fatalf("Build(%q) unexpectedly truncated", message)
	}
	return out
}

func assertRecallFactLifecycle(t *testing.T, store *wiki.Store, name, message, want, stale string) {
	t.Helper()
	out := buildRecallFactLifecycle(t, store, message)
	if got := strings.Count(out, stale); got != 0 {
		t.Errorf("%s: stale value occurred %d times, want 0:\n%s", name, got, out)
	}
	if want != "" && !strings.Contains(out, want) {
		t.Errorf("%s: current value %q missing:\n%s", name, want, out)
	}
}
