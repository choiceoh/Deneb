package enginecontrol

import (
	"strings"
	"testing"
	"time"
)

func yes() *bool { v := true; return &v }
func no() *bool  { v := false; return &v }

// The router plus the Qwen3.8 pair: thinking on and off at the engine, and
// Qwen3.8's entries marked text-only the way they were before ST served its
// pictures (#1274) — the operator's vision:false, which nothing outranks.
var engineEntries = []Entry{
	{Name: "glm-5.3-flash", UpstreamModel: "glm-5.3-flash", ThinkingMode: "on", Vision: yes()},
	{Name: "glm-5.3-flash-low", UpstreamModel: "glm-5.3-flash", ThinkingMode: "off", Vision: yes()},
	{Name: "qwen3.8-flash-next", UpstreamModel: "qwen3.8-flash-next", ThinkingMode: "on", Vision: no()},
	{Name: "qwen3.8-flash-next-low", UpstreamModel: "qwen3.8-flash-next", ThinkingMode: "off", Vision: no()},
}

var liveRoles = map[string]string{
	"main":        "kimi/k3",
	"coding":      "wormhole/glm-5.3-flash",
	"lightweight": "wormhole/glm-5.3-flash",
	"submain":     "wormhole/glm-5.3-flash",
	"tiny":        "wormhole/glm-5.3-flash-low",
	"vision":      "wormhole/glm-5.3-flash",
	"fallback":    "wormhole/deepseek-v4-flash-api",
}

func movesOf(moves []Move) string {
	parts := make([]string, 0, len(moves))
	for _, m := range moves {
		parts = append(parts, m.Role+":"+strings.TrimPrefix(m.From, "wormhole/")+"->"+strings.TrimPrefix(m.To, "wormhole/"))
	}
	return strings.Join(parts, " ")
}

func TestTheLocalRolesFollowTheEngineToQwen38ByVariant(t *testing.T) {
	moves, skips := PlanFollow(liveRoles, engineEntries, "qwen3.8-flash-next", nil)
	want := "coding:glm-5.3-flash->qwen3.8-flash-next lightweight:glm-5.3-flash->qwen3.8-flash-next " +
		"submain:glm-5.3-flash->qwen3.8-flash-next tiny:glm-5.3-flash-low->qwen3.8-flash-next-low"
	if got := movesOf(moves); got != want {
		t.Fatalf("moves = %s\nwant    %s", got, want)
	}
	if len(skips) != 1 || skips[0].Role != "vision" || !strings.Contains(skips[0].Reason, "이미지를 받지 않습니다") {
		t.Fatalf("skips = %+v, want vision held back from a text-only door", skips)
	}
}

func TestTheRolesFollowBackToGLM53(t *testing.T) {
	onQwen := map[string]string{
		"coding": "wormhole/qwen3.8-flash-next", "tiny": "wormhole/qwen3.8-flash-next-low",
		"vision": "wormhole/glm-5.3-flash", "main": "kimi/k3",
	}
	moves, skips := PlanFollow(onQwen, engineEntries, "glm-5.3-flash", nil)
	if got := movesOf(moves); got != "coding:qwen3.8-flash-next->glm-5.3-flash tiny:qwen3.8-flash-next-low->glm-5.3-flash-low" {
		t.Fatalf("moves = %s", got)
	}
	if len(skips) != 0 {
		t.Errorf("skips = %+v: vision already asks for what the engine serves", skips)
	}
}

func TestNothingMovesWithoutAnEntryOfTheSameVariant(t *testing.T) {
	entries := engineEntries[:3] // no Qwen3.8 thinking-off entry
	moves, skips := PlanFollow(liveRoles, entries, "qwen3.8-flash-next", nil)
	for _, m := range moves {
		if m.Role == "tiny" {
			t.Fatalf("tiny moved to %s: a thinking-off role must not land on a thinking entry", m.To)
		}
	}
	var tiny *Skip
	for i := range skips {
		if skips[i].Role == "tiny" {
			tiny = &skips[i]
		}
	}
	if tiny == nil || !strings.Contains(tiny.Reason, "생각 끈") {
		t.Fatalf("skips = %+v, want tiny skipped for a missing thinking-off entry", skips)
	}
}

func TestRolesOffTheEngineNeverMove(t *testing.T) {
	moves, _ := PlanFollow(map[string]string{
		"main": "kimi/k3", "fallback": "wormhole/deepseek-v4-flash-api", "tinyfallback": "openrouter/nvidia/nemotron-3-super-120b-a12b:free",
	}, engineEntries, "qwen3.8-flash-next", nil)
	if len(moves) != 0 {
		t.Fatalf("moves = %+v", moves)
	}
	if moves, skips := PlanFollow(liveRoles, engineEntries, "", nil); moves != nil || skips != nil {
		t.Fatal("nothing served, nothing moves")
	}
	if moves, _ := PlanFollow(liveRoles, engineEntries, "glm-5.3-flash", nil); len(moves) != 0 {
		t.Fatalf("the engine serves what the roles ask for: moves = %+v", moves)
	}
}

// Vision is held back on every pass while the engine serves a text-only model;
// those passes must not erase the record of the switch that moved the rest.
func TestTheFollowLogKeepsTheMovesAcrossPassesThatOnlyHoldBack(t *testing.T) {
	var log FollowLog
	at := time.Unix(1_789_800_000, 0)
	moves, skips := PlanFollow(liveRoles, engineEntries, "qwen3.8-flash-next", nil)
	log.Record(FollowReport{At: at, Served: "qwen3.8-flash-next", Moves: moves, Skips: skips})
	log.Record(FollowReport{At: at.Add(30 * time.Second), Served: "qwen3.8-flash-next", Skips: skips})
	got := log.Last()
	if !got.At.Equal(at) || len(got.Moves) != 4 || len(got.Skips) != 1 {
		t.Fatalf("last = %+v, want the switch's four moves and the one held back", got)
	}
	log.Record(FollowReport{At: at.Add(time.Hour), Served: "glm-5.3-flash"}) // back on GLM-5.3: nothing held back
	if got := log.Last(); len(got.Skips) != 0 || len(got.Moves) != 4 {
		t.Fatalf("after a quiet pass = %+v, want the held-back list cleared and the moves kept", got)
	}
	var nilLog *FollowLog
	nilLog.Record(FollowReport{Moves: moves})
	if got := nilLog.Last(); got.Moves != nil {
		t.Fatal("a nil log reports nothing")
	}
}

// Qwen3.8's router entries no longer promise anything: whether vision goes with
// the engine is the engine's own report for this boot (ST's capabilities.vision,
// true only when every rank bound the tower).
func TestVisionFollowsTheEnginesOwnReport(t *testing.T) {
	entries := []Entry{
		{Name: "glm-5.3-flash", UpstreamModel: "glm-5.3-flash", ThinkingMode: "on", Vision: yes()},
		{Name: "glm-5.3-flash-low", UpstreamModel: "glm-5.3-flash", ThinkingMode: "off", Vision: yes()},
		{Name: "qwen3.8-flash-next", UpstreamModel: "qwen3.8-flash-next", ThinkingMode: "on"},
		{Name: "qwen3.8-flash-next-low", UpstreamModel: "qwen3.8-flash-next", ThinkingMode: "off"},
	}
	vision := map[string]string{"vision": "wormhole/glm-5.3-flash"}
	reason := func(skips []Skip) string {
		if len(skips) != 1 {
			return ""
		}
		return skips[0].Reason
	}

	moves, skips := PlanFollow(vision, entries, "qwen3.8-flash-next", yes())
	if movesOf(moves) != "vision:glm-5.3-flash->qwen3.8-flash-next" || len(skips) != 0 {
		t.Errorf("a boot with its tower: moves %s skips %+v", movesOf(moves), skips)
	}
	moves, skips = PlanFollow(vision, entries, "qwen3.8-flash-next", no())
	if len(moves) != 0 || reason(skips) != "이번 부팅의 엔진이 이미지를 받지 않습니다 (비전 타워 없음)" {
		t.Errorf("a text-only boot: moves %s skips %+v", movesOf(moves), skips)
	}
	moves, skips = PlanFollow(vision, entries, "qwen3.8-flash-next", nil)
	if len(moves) != 0 || !strings.Contains(reason(skips), "vision 표시가 없습니다") {
		t.Errorf("no report, no promise: moves %s skips %+v", movesOf(moves), skips)
	}
	// The operator's vision:false outranks a boot that could see.
	forbid := append([]Entry{}, entries...)
	forbid[2].Vision = no()
	moves, skips = PlanFollow(vision, forbid, "qwen3.8-flash-next", yes())
	if len(moves) != 0 || reason(skips) != "qwen3.8-flash-next 엔트리는 이미지를 받지 않습니다" {
		t.Errorf("forbidden: moves %s skips %+v", movesOf(moves), skips)
	}
	// And an entry's promise does not outvote an engine that says no.
	promise := append([]Entry{}, entries...)
	promise[2].Vision = yes()
	if moves, _ := PlanFollow(vision, promise, "qwen3.8-flash-next", no()); len(moves) != 0 {
		t.Errorf("vision:true moved vision onto a text-only boot: %s", movesOf(moves))
	}
}
