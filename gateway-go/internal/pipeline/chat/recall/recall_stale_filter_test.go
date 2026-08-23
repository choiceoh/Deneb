package recall

import (
	"path/filepath"
	"testing"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

func TestStripSupersededPageMarkersDoesNotPoisonStructuralHeadings(t *testing.T) {
	evidence := []recallEvidence{
		{
			Kind:       "superseded",
			StaleValue: "# 에이콘 상사 담당자\n\n## 핵심 사실\n\n- 에이콘 담당자는 이태호 차장이다\n\n## 변경 이력",
		},
		{Kind: "wiki", Note: "새 페이지의 핵심 사실: 에이콘 담당자는 박수진 과장이다"},
	}
	kept, stale := stripSupersededPageMarkers(evidence)
	if len(kept) != 1 || kept[0].Kind != "wiki" {
		t.Fatalf("kept = %+v", kept)
	}
	for _, value := range stale {
		if structuralStaleValue(value) || value == "에이콘 상사 담당자" {
			t.Fatalf("structural heading became stale deny phrase: %q in %#v", value, stale)
		}
	}
	filtered := filterStaleFactEvidence(kept, append(stale, "핵심 사실"))
	if len(filtered) != 1 {
		t.Fatalf("structural stale phrase removed current evidence: %+v", filtered)
	}
}

func TestStripSupersededPageMarkersRecordsButDoesNotGloballyDenyShortAtomicValues(t *testing.T) {
	evidence := []recallEvidence{
		{Kind: "superseded", StaleValue: "# 이전 상태\n\n- A"},
		{Kind: "diary", Note: "예전 릴리스 상태는 A였다"},
		{Kind: "diary", Note: "alpha project remains active"},
	}
	kept, stale := stripSupersededPageMarkers(evidence)
	if len(stale) != 1 || stale[0] != "A" {
		t.Fatalf("short marker stale values = %#v, want A", stale)
	}
	filtered := filterStaleFactEvidence(kept, stale)
	if len(filtered) != 2 {
		t.Fatalf("short untyped stale marker removed unrelated evidence: %+v", filtered)
	}
}

func TestFilterRecallFactLifecycleEvidenceScopesSharedShortValues(t *testing.T) {
	root := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(root, "wiki"), filepath.Join(root, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	upsert := func(input wiki.FactInput) {
		t.Helper()
		if _, upsertErr := store.UpsertFact(input); upsertErr != nil {
			t.Fatal(upsertErr)
		}
	}
	upsert(wiki.FactInput{
		Subject: "project:alpha", Key: "quote.amount", Value: "A",
		Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha"}, At: base, BasisAt: base,
	})
	for index, value := range []string{"A", "B"} {
		at := base.Add(time.Duration(index) * time.Hour)
		upsert(wiki.FactInput{
			Subject: "project:beta", Key: "quote.amount", Value: value,
			Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
			Sources: []string{"doc:beta"}, At: at, BasisAt: at,
		})
	}
	makeEvidence := func() []recallEvidence {
		return []recallEvidence{
			{Kind: "file", Source: "/project/alpha/quote.txt", Note: "project alpha quote amount A"},
			{Kind: "diary", Source: "diary#beta-old", Note: "project beta quote amount A"},
			{Kind: "transcript", Source: "session:beta-current", Note: "project beta quote amount B"},
			{Kind: "file", Source: "/quality/grade.txt", Note: "quality grade A passes"},
		}
	}
	filtered := filterRecallFactLifecycleEvidence(store, "project beta quote amount", makeEvidence(), store.RecallFactSnapshot())
	if len(filtered) != 3 || filtered[0].Source != "/project/alpha/quote.txt" ||
		filtered[1].Source != "session:beta-current" || filtered[2].Source != "/quality/grade.txt" {
		t.Fatalf("identity-scoped recall filter = %+v", filtered)
	}
	if _, err := store.TombstoneFact(wiki.FactTombstoneInput{
		Subject: "project:beta", Key: "quote.amount",
		Authority: wiki.FactAuthorityAgent, At: base.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	filtered = filterRecallFactLifecycleEvidence(store, "project beta quote amount", makeEvidence(), store.RecallFactSnapshot())
	if len(filtered) != 2 || filtered[0].Source != "/project/alpha/quote.txt" || filtered[1].Source != "/quality/grade.txt" {
		t.Fatalf("identity-scoped tombstone filter = %+v", filtered)
	}
}

func TestFilterUntypedStaleFactEvidenceDoesNotGloballyDenyShortValues(t *testing.T) {
	evidence := []recallEvidence{
		{Kind: "diary", Note: "alpha project remains active"},
		{Kind: "diary", Note: "legacy status was A"},
		{Kind: "diary", Note: "예산은 800만원이다"},
		{Kind: "diary", Note: "이전 예산은 80 만원이다"},
		{Kind: "diary", Note: "Bobcat 배포를 점검했다"},
		{Kind: "diary", Note: "owner was Bob"},
	}

	filtered := filterStaleFactEvidence(evidence, []string{"A", "80", "Bob"})
	if len(filtered) != len(evidence) {
		t.Fatalf("short untyped values became global denies: %+v", filtered)
	}
}

func TestEvidenceContainsNormalizedFactValueHandlesAttachedParticlesAndUnits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		note  string
		value string
		want  bool
	}{
		{name: "latin particle", note: "예전 상태는 A는 아니었다", value: "A", want: true},
		{name: "latin past particle", note: "예전 상태는 B였다", value: "B", want: true},
		{name: "latin polite past", note: "예전 상태는 A였어요", value: "A", want: true},
		{name: "latin contrastive past", note: "예전 상태는 A였는데 지금은 다르다", value: "A", want: true},
		{name: "latin attributive past", note: "A였던 릴리스 상태", value: "A", want: true},
		{name: "hangul polite past", note: "예전 단계는 구버전이었어요", value: "구버전", want: true},
		{name: "hangul contrastive past", note: "구버전이었는데 교체됐다", value: "구버전", want: true},
		{name: "hangul attributive past", note: "구버전이었던 배포", value: "구버전", want: true},
		{name: "numeric unit particle", note: "마감은 9시였다", value: "9", want: true},
		{name: "numeric currency unit", note: "예전 예산은 80만원", value: "80", want: true},
		{name: "punctuated value", note: "예전 상태는 N/A였다", value: "N/A", want: true},
		{name: "hyphenated value", note: "예전 단계는 A-1이었다", value: "A-1", want: true},
		{name: "hyphenated left component", note: "예전 단계는 A-1이었다", value: "A", want: false},
		{name: "hyphenated right component", note: "예전 단계는 A-1이었다", value: "1", want: false},
		{name: "symbol value", note: "예전 표시는 ✅였다", value: "✅", want: true},
		{name: "latin substring", note: "alpha project remains active", value: "A", want: false},
		{name: "numeric prefix", note: "예산은 800만원", value: "80", want: false},
		{name: "word prefix", note: "Bobcat 배포", value: "Bob", want: false},
		{name: "numeric prefix time", note: "마감은 90시", value: "9", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := evidenceContainsNormalizedFactValue(tc.note, tc.value); got != tc.want {
				t.Fatalf("evidenceContainsNormalizedFactValue(%q, %q) = %v, want %v", tc.note, tc.value, got, tc.want)
			}
		})
	}
}
