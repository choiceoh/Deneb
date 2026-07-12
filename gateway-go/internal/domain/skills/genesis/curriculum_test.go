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
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	task := &CurriculumTask{
		Tracker:   tr,
		Logger:    slog.Default(),
		proposeFn: func(context.Context, string) (curriculumResp, error) { return resp, nil },
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
func TestCurriculumRun_SkipFilesNothing(t *testing.T) {
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
