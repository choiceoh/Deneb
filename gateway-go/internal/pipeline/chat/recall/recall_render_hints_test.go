package recall

import (
	"strings"
	"testing"
	"time"
)

// The session follow-up hint mirrors the files rule: name the route only when
// the preset can take it. The aggregation scaffold is unconditional — it
// instructs reading, not tool use.
func TestSessionOpenHintGatedByReachability(t *testing.T) {
	ev := []recallEvidence{{Kind: "session", Source: "cl:x:s3#1/user", Note: "n", Score: 0.9, At: 1}}
	reachable, _ := formatRecallEvidenceAt(ev, time.Now(), true, true)
	if !strings.Contains(reachable, `sessions(action="history"`) {
		t.Fatalf("reachable preset must name the sessions.history route:\n%s", reachable)
	}
	restricted, _ := formatRecallEvidenceAt(ev, time.Now(), true, false)
	if strings.Contains(restricted, `sessions(action="history"`) {
		t.Fatalf("restricted preset must not name an unreachable route:\n%s", restricted)
	}
	for _, block := range []string{reachable, restricted} {
		if !strings.Contains(block, "먼저 나열한 뒤") {
			t.Fatalf("aggregation scaffold must render unconditionally:\n%s", block)
		}
	}
}

// Soft per-conversation decay must spread the budget across conversations
// without the removed hard cap's failure mode: a decayed row still wins when
// its base score carries it, and the top pick is never affected.
func TestSessionDecaySpreadsBudgetAcrossConversations(t *testing.T) {
	build := func() []recallEvidence {
		return []recallEvidence{
			{Kind: "session", Source: "a#1/user", Note: "alpha one", Score: 1.0},
			{Kind: "session", Source: "a#2/user", Note: "alpha two", Score: 0.9},
			{Kind: "session", Source: "a#3/user", Note: "alpha three", Score: 0.8},
			{Kind: "session", Source: "b#1/user", Note: "beta one", Score: 0.7},
		}
	}

	t.Setenv("DENEB_POLARIS_SESSION_DECAY", "1")
	picked := cutToBudgetWithDiversity(build(), 3)
	if picked[0].Source != "a#1/user" || picked[1].Source != "a#2/user" || picked[2].Source != "a#3/user" {
		t.Fatalf("decay off must keep pure rank order, got %s/%s/%s",
			picked[0].Source, picked[1].Source, picked[2].Source)
	}

	t.Setenv("DENEB_POLARIS_SESSION_DECAY", "0.7")
	picked = cutToBudgetWithDiversity(build(), 3)
	if picked[0].Source != "a#1/user" {
		t.Fatalf("top pick must be decay-invariant, got %s", picked[0].Source)
	}
	// After one a-row, a#2 decays to 0.63 < b's 0.7 → b jumps the queue;
	// a#2 (still 0.63 > a#3's 0.56) takes the last slot.
	if picked[1].Source != "b#1/user" || picked[2].Source != "a#2/user" {
		t.Fatalf("decay must promote the unrendered conversation, got %s/%s",
			picked[1].Source, picked[2].Source)
	}
}

// Bare-entity lookups must survive cue tokenization: the 2-rune cue "이어"
// used to fire INSIDE 금호타이어, flagging the message as a cue turn and
// eating the entity as a stopword — queries came back EMPTY and recall
// rendered nothing (Korean probe, measured). "이어서" still cues.
func TestBareEntitySurvivesCueTokenization(t *testing.T) {
	if hasCue("금호타이어") {
		t.Fatalf("a noun containing 이어 must not read as a recall cue")
	}
	queries := searchQueries("금호타이어")
	if len(queries) == 0 || !strings.Contains(strings.Join(queries, " "), "금호타이어") {
		t.Fatalf("the entity must survive as a query, got %v", queries)
	}
	if !hasCue("아까 하던 얘기 이어서 계속해줘") {
		t.Fatalf("이어서 must still cue")
	}
}
