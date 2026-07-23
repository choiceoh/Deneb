package genesis

import (
	"log/slog"
	"strings"
	"testing"
)

func toolGapResp(tool, description, fix string) evolveResp {
	resp := evolveResp{Skip: true, Reason: "도구 결함이 근본 원인"}
	resp.ToolGap = &struct {
		Tool        string `json:"tool,omitempty"`
		Description string `json:"description,omitempty"`
		ProposedFix string `json:"proposed_fix,omitempty"`
	}{Tool: tool, Description: description, ProposedFix: fix}
	return resp
}

func toolGapStats(tool string) *UsageStats {
	return &UsageStats{
		SkillName: "sk",
		RecentFailureTraces: []UsageFailureTrace{
			{Signature: "wiki_search timeout on long queries", ToolName: tool, ToolError: true, ErrorMsg: "deadline exceeded"},
		},
	}
}

// P4 pairing: a grounded tool-gap declaration emits ONE coding candidate and a
// lifecycle pairing entry; hallucinated or duplicate declarations do not.
func TestMaybePairToolGapQueuesGroundedDeclarationOnceSkipsHallucinatedOrMalformed(t *testing.T) {
	newEvolver := func(t *testing.T) *Evolver {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		tr, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		return &Evolver{tracker: tr, logger: slog.Default()}
	}
	queued := func(t *testing.T, e *Evolver) []SelfCorrectionCandidateRecord {
		t.Helper()
		got, err := e.tracker.RecentSelfCorrectionCandidates("sk", "", 10)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	pairedEntries := func(t *testing.T, e *Evolver) int {
		t.Helper()
		entries, err := e.tracker.RecentLifecycleLog(20)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, entry := range entries {
			if entry.Type == evolveToolGapPairedType && entry.SkillName == "sk" {
				n++
			}
		}
		return n
	}

	t.Run("grounded gap pairs once", func(t *testing.T) {
		e := newEvolver(t)
		e.maybePairToolGap("sk", toolGapResp("wiki_search", "타임아웃 파라미터가 없다", "타임아웃 옵션 추가"), toolGapStats("wiki_search"), "")
		got := queued(t, e)
		if len(got) != 1 || got[0].Source != "evolve-tool-gap" || got[0].Scope != "code" {
			t.Fatalf("paired candidate = %+v", got)
		}
		if !strings.Contains(got[0].Evidence, "wiki_search timeout") {
			t.Fatalf("evidence not grounded in the failure trace: %q", got[0].Evidence)
		}
		if pairedEntries(t, e) != 1 {
			t.Fatal("lifecycle pairing entry missing")
		}

		// Same declaration again: the open candidate blocks a duplicate.
		e.maybePairToolGap("sk", toolGapResp("wiki_search", "타임아웃 파라미터가 없다", ""), toolGapStats("wiki_search"), "")
		if len(queued(t, e)) != 1 {
			t.Fatal("duplicate pairing queued")
		}
	})

	t.Run("carries the spawning evolve procedure ref", func(t *testing.T) {
		e := newEvolver(t)
		const ver = "abc123def456" // captured evolve-prompt version from the producer snapshot
		e.maybePairToolGap("sk", toolGapResp("wiki_search", "타임아웃 파라미터가 없다", "타임아웃 옵션 추가"), toolGapStats("wiki_search"), ver)
		got := queued(t, e)
		want := candidateProcedureRef(ver)
		if len(got) != 1 {
			t.Fatalf("want 1 candidate, got %d", len(got))
		}
		if want == "" || got[0].ProcedureRef != want {
			t.Fatalf("candidate procedure ref = %q; want %q", got[0].ProcedureRef, want)
		}
		// An empty (deterministic / unknown-producer) version yields no ref.
		if candidateProcedureRef("") != "" {
			t.Fatal("empty evolve version must yield an empty procedure ref")
		}
	})

	t.Run("ungrounded tool is dropped", func(t *testing.T) {
		e := newEvolver(t)
		e.maybePairToolGap("sk", toolGapResp("invented_tool", "환각 도구", ""), toolGapStats("wiki_search"), "")
		if len(queued(t, e)) != 0 {
			t.Fatal("hallucinated tool gap reached the coding queue")
		}
	})

	t.Run("malformed and nil declarations are no-ops", func(t *testing.T) {
		e := newEvolver(t)
		e.maybePairToolGap("sk", toolGapResp("", "설명", ""), toolGapStats("wiki_search"), "")
		e.maybePairToolGap("sk", toolGapResp("wiki_search", "", ""), toolGapStats("wiki_search"), "")
		e.maybePairToolGap("sk", evolveResp{Skip: true}, toolGapStats("wiki_search"), "")
		e.maybePairToolGap("sk", toolGapResp("wiki_search", "설명", ""), nil, "")
		if len(queued(t, e)) != 0 {
			t.Fatal("malformed declaration reached the queue")
		}
	})
}
