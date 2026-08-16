package genesis

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func curriculumFixture(t *testing.T, resp curriculumResp, catalog map[string]string) (*CurriculumTask, *Tracker) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	// A fixed environment signal, shaped like a real failed-request bullet so
	// the grounding corpus keeps the QUOTED MESSAGE (not the section header).
	// The fixture quotes a slice of that message — the actual demand data.
	const envDemand = "투자사 미팅 사전 브리프를 만들어줘"
	const envSignal = "최근 실패한 요청(명시적 능력 갭):\n- 07-12: \"" + envDemand + "\" — 오류: 해당 능력 없음"
	task := &CurriculumTask{
		Tracker:   tr,
		Logger:    slog.Default(),
		EnvDigest: func(context.Context) string { return envSignal },
		proposeFn: func(_ context.Context, _ string) (curriculumResp, error) {
			// Ground the fixture's evidence in the actual demand data (not the
			// scaffolding header) — the grounding gate needs a >=12-rune
			// verbatim quote from the env-derived corpus.
			out := resp
			out.Evidence = "인용: " + envDemand
			return out, nil
		},
		catalogFn: func() map[string]string { return catalog },
	}
	return task, tr
}

func admissibleCurriculumResp() curriculumResp {
	return curriculumResp{
		Name:     "meeting-brief-digest",
		Brief:    strings.Repeat("회의 전 참석자·안건·직전 이력을 모아 한 장 브리프를 만든다. ", 3),
		Evidence: "일정 카드에 회의가 반복되나 사전 브리프 스킬이 없다",
		Reason:   "coverage gap",
		Cases: []curriculumCase{
			{
				Description:        "내일 회의 브리프",
				Input:              "내일 오전 미팅 브리프 만들어줘",
				RequiredSubstrings: []string{"참석자", "안건"},
			},
		},
	}
}

// The lane's contract: an admissible proposal files the validation cases FIRST
// and then exactly one route=genesis opportunity, both provenance-tagged.
func TestCurriculumRun_FilesOpportunityWithCasesFirst(t *testing.T) {
	task, tr := curriculumFixture(t, admissibleCurriculumResp(), map[string]string{
		"existing-skill": "already here",
	})
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	opps, err := tr.RecentSkillOpportunities("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(opps) != 1 {
		t.Fatalf("opportunities = %d, want 1 (%+v)", len(opps), opps)
	}
	got := opps[0]
	if got.Route != "genesis" || got.Source != curriculumSourceTag || got.SkillName != "meeting-brief-digest" {
		t.Fatalf("opportunity = %+v", got)
	}
	if !strings.HasPrefix(got.Candidate, "meeting-brief-digest: ") {
		t.Fatalf("candidate not name-prefixed: %q", got.Candidate)
	}

	cases, err := tr.RecentSkillValidationCases("meeting-brief-digest", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 {
		t.Fatalf("validation cases = %d, want 1", len(cases))
	}
	if cases[0].Source != curriculumSourceTag {
		t.Fatalf("case source = %q, want %q", cases[0].Source, curriculumSourceTag)
	}
	if cases[0].Replay.Input == "" || len(cases[0].RequiredSubstrings) == 0 {
		t.Fatalf("case lost its oracle: %+v", cases[0])
	}
}

// A skip verdict files nothing — zero opportunities, zero cases.
func TestCurriculumRun_SkipVerdictWritesNoOpportunitiesOrCases(t *testing.T) {
	task, tr := curriculumFixture(t, curriculumResp{Skip: true, Reason: "충분히 덮여 있음"}, nil)
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	opps, err := tr.RecentSkillOpportunities("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(opps) != 0 {
		t.Fatalf("skip must file nothing, got %+v", opps)
	}
}

// Re-running with the same proposal is idempotent: the backlog dedup gate
// (SkillName match inside the window) drops the echo, so a restart or a
// double-fire cannot flood the backlog with the same demand.
func TestCurriculumRun_RerunIsIdempotent(t *testing.T) {
	task, tr := curriculumFixture(t, admissibleCurriculumResp(), nil)
	for i := 0; i < 3; i++ {
		if err := task.Run(context.Background()); err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
	}
	opps, err := tr.RecentSkillOpportunities("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(opps) != 1 {
		t.Fatalf("opportunities = %d, want 1 after reruns", len(opps))
	}
	cases, err := tr.RecentSkillValidationCases("meeting-brief-digest", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 {
		t.Fatalf("cases = %d, want 1 after reruns (cases must not be re-filed either)", len(cases))
	}
}

// The deterministic gate: catalog overlap, missing oracle, and skill-body
// smuggling are all inadmissible regardless of what the producer claims.
func TestCurriculumGate_Rejections(t *testing.T) {
	base := admissibleCurriculumResp()
	now := time.Now()

	overlap := base
	overlap.Name = "Existing Skill" // normalizes to existing-skill
	if reason := curriculumGate(overlap, map[string]string{"existing-skill": "x"}, nil, now); !strings.Contains(reason, "already in the catalog") {
		t.Fatalf("catalog overlap not rejected: %q", reason)
	}

	caseless := base
	caseless.Cases = nil
	if reason := curriculumGate(caseless, nil, nil, now); !strings.Contains(reason, "held-out oracle") {
		t.Fatalf("caseless proposal not rejected: %q", reason)
	}

	noAssert := base
	noAssert.Cases = []curriculumCase{{Input: "질문"}}
	if reason := curriculumGate(noAssert, nil, nil, now); !strings.Contains(reason, "no assertion") {
		t.Fatalf("assertion-less case not rejected: %q", reason)
	}

	smuggled := base
	smuggled.Brief = strings.Repeat("very long body ", 200)
	if reason := curriculumGate(smuggled, nil, nil, now); !strings.Contains(reason, "too large") {
		t.Fatalf("skill-body smuggling not rejected: %q", reason)
	}

	backlogHit := base
	backlog := []SkillOpportunityRecord{{
		SkillName: "meeting-brief-digest",
		Candidate: "meeting-brief-digest: something",
		CreatedAt: now.UnixMilli(),
	}}
	if reason := curriculumGate(backlogHit, nil, backlog, now); !strings.Contains(reason, "already in the backlog") {
		t.Fatalf("backlog echo not rejected: %q", reason)
	}

	// The dedup window expires: the same capability is admissible again after
	// the window so long-dead demand can be re-raised.
	stale := []SkillOpportunityRecord{{
		SkillName: "meeting-brief-digest",
		Candidate: "meeting-brief-digest: something",
		CreatedAt: now.Add(-curriculumDedupWindow - time.Hour).UnixMilli(),
	}}
	if reason := curriculumGate(base, nil, stale, now); reason != "" {
		t.Fatalf("expired backlog entry must not block: %q", reason)
	}
}

func TestNormalizeCurriculumName(t *testing.T) {
	for in, want := range map[string]string{
		"Meeting Brief Digest": "meeting-brief-digest",
		"  weird__name!!":      "weird-name",
		"한글이름":                 "",
	} {
		if got := normalizeCurriculumName(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// Source-grounding gate (SkillCenter 2607.07676): the evidence field must
// quote the demand-evidence block verbatim; hallucinated justifications and
// too-short evidence are rejected regardless of plausibility.
func TestCurriculumSourceGrounding_AcceptsVerbatimQuoteRejectsHallucinatedOrShortEvidence(t *testing.T) {
	input := "## 현재 스킬 카탈로그\n- mail-triage — 메일 분류와 우선순위 정리\n## 환경 다이제스트\n다음 주 화요일 투자사 미팅 준비 항목 5건이 위키에 기록됨"

	if got := curriculumSourceGrounding("위키에 따르면 투자사 미팅 준비 항목 5건이 위키에 기록됨 — 반복 수요", input); got != "" {
		t.Fatalf("verbatim quote must pass: %s", got)
	}
	if got := curriculumSourceGrounding("운영자가 매주 보고서를 요청한다는 강한 신호가 있다", input); got == "" {
		t.Fatal("hallucinated evidence must be rejected")
	}
	if got := curriculumSourceGrounding("짧음", input); got == "" {
		t.Fatal("too-short evidence must be rejected")
	}
}

// Codex review: the grounding corpus keeps only demand DATA — bullet content
// and the payload of a "header: data" line — never the static section headers,
// so quoting a boilerplate header cannot satisfy grounding.
func TestCurriculumGroundingLines_PreservesDemandDataStripsSectionHeaders(t *testing.T) {
	digest := "최근 실패한 요청(명시적 능력 갭, 최대 5):\n" +
		"- 07-12: \"투자사 미팅 브리프를 만들어줘\" — 오류: 없음\n" +
		"\n활성 위키 상대 도메인(최대 10): acme.com · bohae.co.kr\n" +
		"다가오는 일정(스킬 커버리지 갭 후보, 최대 5):\n" +
		"- 07-15: 분기 실적 발표 준비"
	corpus := curriculumGroundingLines(digest)
	// Pure headers are gone.
	if strings.Contains(corpus, "최근 실패한 요청") || strings.Contains(corpus, "다가오는 일정") {
		t.Fatalf("section headers survived into the grounding corpus:\n%s", corpus)
	}
	// Demand data survives: quoted message, wiki domains, calendar summary.
	for _, want := range []string{"투자사 미팅 브리프를 만들어줘", "acme.com · bohae.co.kr", "분기 실적 발표 준비"} {
		if !strings.Contains(corpus, want) {
			t.Fatalf("demand data %q missing from grounding corpus:\n%s", want, corpus)
		}
	}
	// A proposal quoting only a header fails grounding; quoting data passes.
	if reason := curriculumSourceGrounding("최근 실패한 요청(명시적 능력 갭", corpus); reason == "" {
		t.Fatal("quoting a section header must NOT ground")
	}
	if reason := curriculumSourceGrounding("투자사 미팅 브리프를 만들어줘", corpus); reason != "" {
		t.Fatalf("quoting real demand data must ground: %s", reason)
	}
}

// M6: grounding scans ONLY the environment-derived corpus. A proposal quoting
// the self-authored scaffolding (a section header, a catalog line) proves no
// real demand and must be rejected; only a quote from the env digest grounds.
func TestCurriculumRun_GroundingRejectsScaffoldingQuote(t *testing.T) {
	scaffoldQuote := curriculumResp{
		Name:     "meeting-brief-digest",
		Brief:    strings.Repeat("회의 전 참석자·안건을 모아 브리프를 만든다. ", 3),
		Reason:   "coverage gap",
		Evidence: "## 이미 알려진 수요 백로그 (재제안 금지)", // pure scaffolding, in the full block only
		Cases: []curriculumCase{{
			Description: "브리프", Input: "브리프 만들어줘", RequiredSubstrings: []string{"참석자", "안건"},
		}},
	}
	// HOME before NewTracker: the tracker resolves ~/.deneb at construction, so
	// setting HOME afterward would read the real user ledgers (Codex review).
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	task := &CurriculumTask{
		Tracker: tr, Logger: slog.Default(),
		EnvDigest: func(context.Context) string {
			return "환경: 반복되는 투자사 미팅 준비 요청이 관찰됨"
		},
		proposeFn: func(context.Context, string) (curriculumResp, error) { return scaffoldQuote, nil },
		catalogFn: func() map[string]string { return map[string]string{} },
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	opps, _ := tr.RecentSkillOpportunities("", 10)
	if len(opps) != 0 {
		t.Fatalf("scaffolding-grounded proposal must be rejected, filed %d opportunities", len(opps))
	}
}
