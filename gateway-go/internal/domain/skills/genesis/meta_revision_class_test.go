package genesis

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

// The classifier is the L1.5-trap telemetry's foundation: structural means the
// prompt's skeleton (headings/numbered rules) or a substantial share of its
// body changed; parametric means tweaks inside the existing skeleton. It must
// be deterministic and err toward structural only on real skeleton changes.
func TestClassifyMetaRevision(t *testing.T) {
	incumbent := `당신은 개선자입니다.

## 원칙
1. 한 사이클에 한 가지 약점만 고친다
2. 증거 우선: 스코어보드가 가리키는 약점만 겨냥한다
3. 출력 스키마는 바꾸지 않는다

## 출력 (JSON만)
{"skip": false}`

	cases := []struct {
		name      string
		proposal  string
		wantClass string
		wantIn    string // substring of detail
	}{
		{
			"숫자 임계값만 손질 → parametric",
			strings.Replace(incumbent, "한 가지 약점만", "정확히 한 가지 약점만", 1),
			MetaRevisionClassParametric, "skeleton unchanged",
		},
		{
			"섹션 추가 → structural",
			incumbent + "\n\n## 회귀 점검\n제안 전 회귀 가능성을 명시하라",
			MetaRevisionClassStructural, "skeleton lines",
		},
		{
			"규칙 추가 → structural",
			strings.Replace(incumbent, "3. 출력 스키마는 바꾸지 않는다",
				"3. 출력 스키마는 바꾸지 않는다\n4. 구조적 후보를 우선 검토한다", 1),
			MetaRevisionClassStructural, "skeleton lines",
		},
		{
			// The archetypal L1.5 edit ("quantify rule N"): rewording a rule's
			// text is parametric — only rule presence/count/order is skeleton.
			"규칙 문면 손질 → parametric",
			strings.Replace(incumbent, "2. 증거 우선: 스코어보드가 가리키는 약점만 겨냥한다",
				"2. 증거는 저수율 레버에서만 취한다", 1),
			MetaRevisionClassParametric, "skeleton unchanged",
		},
		{
			"섹션 제목 개명 → structural",
			strings.Replace(incumbent, "## 원칙", "## 불변 원칙", 1),
			MetaRevisionClassStructural, "skeleton line",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			class, detail := classifyMetaRevision(incumbent, tc.proposal)
			if class != tc.wantClass {
				t.Fatalf("class = %s, want %s (detail: %s)", class, tc.wantClass, detail)
			}
			if !strings.Contains(detail, tc.wantIn) {
				t.Fatalf("detail %q does not mention %q", detail, tc.wantIn)
			}
		})
	}
}

// A body rewrite that keeps every heading/rule line but replaces most prose is
// a mechanism rewrite, not a tweak — the ratio arm must catch it.
func TestClassifyMetaRevision_BodyRewriteRatio(t *testing.T) {
	var inc, prop strings.Builder
	inc.WriteString("## 지침\n")
	prop.WriteString("## 지침\n")
	for i := 0; i < 20; i++ {
		inc.WriteString("이 줄은 원본 지침 문장입니다 번호 " + strings.Repeat("가", i+1) + "\n")
		prop.WriteString("완전히 다른 절차를 서술하는 새 문장입니다 번호 " + strings.Repeat("나", i+1) + "\n")
	}
	class, detail := classifyMetaRevision(inc.String(), prop.String())
	if class != MetaRevisionClassStructural {
		t.Fatalf("class = %s, want structural (detail: %s)", class, detail)
	}
	if !strings.Contains(detail, "rewrite ratio") {
		t.Fatalf("detail %q should cite the rewrite ratio", detail)
	}
}

// The balance aggregate joins feed-card adoption records (no class of their
// own) to their proposal record via (artifact, toVersion), tallies the recent
// proposal mix, and counts the newest consecutive parametric adoptions. An
// unresolvable class ends the streak — pre-instrumentation history must read
// as zero, not as continuity.
func TestMetaRevisionClassBalance_StreakAndJoin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	log := func(rec MetaRevisionRecord) {
		t.Helper()
		if err := tr.LogMetaRevision(rec); err != nil {
			t.Fatal(err)
		}
	}
	// Oldest → newest. v1: structural, auto-adopted on the cycle record.
	log(MetaRevisionRecord{
		Epoch: "producer", Artifact: "evolve.md", ToVersion: "v1",
		Proposed: true, Action: "auto_adopted", RevisionClass: MetaRevisionClassStructural,
	})
	// v2: parametric, auto-adopted.
	log(MetaRevisionRecord{
		Epoch: "producer", Artifact: "evolve.md", ToVersion: "v2",
		Proposed: true, Action: "auto_adopted", RevisionClass: MetaRevisionClassParametric,
	})
	// v3: parametric proposal + separate feed-card adoption record (classless).
	log(MetaRevisionRecord{
		Epoch: "producer", Artifact: "evolve.md", ToVersion: "v3",
		Proposed: true, RevisionClass: MetaRevisionClassParametric,
	})
	log(MetaRevisionRecord{Artifact: "evolve.md", ToVersion: "v3", Action: "adopted"})

	bal := tr.MetaRevisionClassBalance()
	if bal.Structural != 1 || bal.Parametric != 2 {
		t.Fatalf("mix = %d structural / %d parametric, want 1/2 (%+v)", bal.Structural, bal.Parametric, bal)
	}
	// Newest-first adoptions: v3(parametric via join), v2(parametric), v1(structural breaks).
	if bal.AdoptedParametricStreak != 2 {
		t.Fatalf("streak = %d, want 2 (%+v)", bal.AdoptedParametricStreak, bal)
	}
}

func TestMetaRevisionClassBalance_UnknownClassEndsStreak(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	// Pre-instrumentation adoption: no class, no joinable proposal.
	if err := tr.LogMetaRevision(MetaRevisionRecord{Artifact: "evolve.md", ToVersion: "old", Action: "auto_adopted"}); err != nil {
		t.Fatal(err)
	}
	if got := tr.MetaRevisionClassBalance().AdoptedParametricStreak; got != 0 {
		t.Fatalf("streak = %d, want 0 for unresolvable class", got)
	}
}

// The producer epoch sees the balance section, and once adoptions run
// parametric for the nudge threshold it must carry the explicit structural
// counter-pressure line; the evaluator epoch must not receive the section at
// all (it revises the judge, not the producer).
func TestAssembleEvidence_RevisionClassNudge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range []string{"v1", "v2", "v3"} {
		if err := tr.LogMetaRevision(MetaRevisionRecord{
			Epoch: "producer", Artifact: "evolve.md", ToVersion: v,
			Proposed: true, Action: "auto_adopted", RevisionClass: MetaRevisionClassParametric,
			CreatedAt: int64(1000 + i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	task := &MetaEvolutionTask{Tracker: tr}
	producer := task.assembleEvidence(context.Background(), metaEpochProducer)
	if !strings.Contains(producer, "개정 구조성 균형") {
		t.Fatalf("producer evidence missing balance section:\n%s", producer)
	}
	if !strings.Contains(producer, "연속 파라미터형") {
		t.Fatalf("producer evidence missing streak nudge (streak=3):\n%s", producer)
	}
	evaluator := task.assembleEvidence(context.Background(), metaEpochEvaluator)
	if strings.Contains(evaluator, "개정 구조성 균형") {
		t.Fatal("evaluator evidence must not carry the producer-side balance section")
	}
}

// Lens rotation must be deterministic and give every early attempt a DISTINCT
// named direction — a generic "be different" note lets the producer repeat its
// prior-favored direction (the Group-A repetition Bilevel Autoresearch traced).
func TestCandidateVariationNote_LensRotation(t *testing.T) {
	seen := map[string]bool{}
	for attempt := 1; attempt <= len(candidateVariationLenses); attempt++ {
		note := candidateVariationNote(attempt)
		if note != candidateVariationNote(attempt) {
			t.Fatalf("attempt %d note not deterministic", attempt)
		}
		if !strings.Contains(note, "검증 계약") {
			t.Fatalf("attempt %d note dropped the contract-preservation tail", attempt)
		}
		if seen[note] {
			t.Fatalf("attempt %d reuses an earlier lens — early attempts must be orthogonal", attempt)
		}
		seen[note] = true
	}
	// Rotation wraps deterministically past the lens list (only the candidate
	// number differs).
	wrapped := candidateVariationNote(len(candidateVariationLenses) + 1)
	if !strings.Contains(wrapped, candidateVariationLenses[0]) {
		t.Fatal("rotation should wrap to the first lens")
	}
}
