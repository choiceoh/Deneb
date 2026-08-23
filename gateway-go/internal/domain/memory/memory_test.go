package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyHeuristics_Targets(t *testing.T) {
	cases := []struct {
		msg    string
		target WriteTarget
		subj   string
	}{
		{"기억해: 나는 저녁을 늦게 먹는 걸 선호해", TargetProfile, SubjectSelf},
		{"앞으로는 이렇게 해줘 — 메일 오면 요약 먼저", TargetProcedure, SubjectSelf},
		{"오늘 회의에서 포트 이야기했어", TargetEpisodic, SubjectSelf},
		{"내 주민번호는 900101-1234567", TargetExclude, SubjectSelf},
		{"아내는 해산물을 못 먹어", TargetProfile, "other:"},
	}
	for _, c := range cases {
		got := ClassifyHeuristics(c.msg)
		if got.Target != c.target {
			t.Errorf("%q: target=%s want %s", c.msg, got.Target, c.target)
		}
		if c.subj == "other:" {
			if !strings.HasPrefix(got.SubjectID, "other:") {
				t.Errorf("%q: subject=%q want other:*", c.msg, got.SubjectID)
			}
		} else if NormalizeSubject(got.SubjectID) != c.subj {
			t.Errorf("%q: subject=%q want %s", c.msg, got.SubjectID, c.subj)
		}
	}
}

func TestFactKeyFromText_CorrectionsShareSemanticAxis(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		key  string
		kind string
	}{
		{
			name: "known response length axis",
			old:  "기억해줘. 나는 답변을 길고 상세하게 원해",
			new:  "앞으로 답변은 짧고 간결하게 해줘",
			key:  "communication.response_length",
			kind: "preference",
		},
		{
			name: "generic preference polarity",
			old:  "기억해줘. 나는 커피를 좋아해",
			new:  "기억해줘. 나는 커피 싫어",
			key:  "커피",
			kind: "preference",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldCandidate := ClassifyHeuristics(tt.old)
			newCandidate := ClassifyHeuristics(tt.new)
			if oldCandidate.FactKey != tt.key || newCandidate.FactKey != tt.key {
				t.Fatalf("keys old=%q new=%q, want %q", oldCandidate.FactKey, newCandidate.FactKey, tt.key)
			}
			if oldCandidate.FactKind != tt.kind || newCandidate.FactKind != tt.kind {
				t.Fatalf("kinds old=%q new=%q, want %q", oldCandidate.FactKind, newCandidate.FactKind, tt.kind)
			}
		})
	}
}

func TestClassifyHeuristics_ExplicitForgetUsesFactTombstoneIntent(t *testing.T) {
	got := ClassifyHeuristics("내 답변 길이 선호는 기억에서 지워줘")
	if got.Target != TargetProfile || !got.Forget {
		t.Fatalf("candidate=%+v, want profile forget", got)
	}
	if got.FactKey != "communication.response_length" || got.FactKind != "preference" {
		t.Fatalf("key=%q kind=%q", got.FactKey, got.FactKind)
	}

	assertion := ClassifyHeuristics("기억해줘. 나는 커피를 좋아해")
	forget := ClassifyHeuristics("내 커피 취향은 잊어줘")
	if !forget.Forget || assertion.FactKey != "커피" || forget.FactKey != assertion.FactKey {
		t.Fatalf("generic assertion=%+v forget=%+v", assertion, forget)
	}

	if remember := ClassifyHeuristics("이 내용은 기억하지 마"); !remember.Forget {
		t.Fatalf("candidate=%+v, explicit do-not-remember must still tombstone", remember)
	}
}

func TestClassifyHeuristics_NegatedForgetDoesNotTombstone(t *testing.T) {
	tests := []struct {
		name string
		msg  string
	}{
		{name: "korean do not delete", msg: "내 간결한 답변 선호를 삭제하지 마"},
		{name: "korean do not delete from memory", msg: "내 답변 길이 선호는 기억에서 삭제하지 마"},
		{name: "korean do not forget", msg: "내 커피 취향은 잊지 마"},
		{name: "korean topic particle", msg: "내 간결한 답변 선호는 삭제하지는 마"},
		{name: "korean limiting particle", msg: "내 커피 취향은 잊지만 마"},
		{name: "korean coordinated verbs", msg: "내 커피 취향은 잊거나 삭제하지 마"},
		{name: "english do not delete", msg: "Do not delete my response length preference from memory"},
		{name: "english do not forget", msg: "Please don't forget my coffee preference"},
		{name: "english adverb", msg: "Please do not permanently delete my preference from memory"},
		{name: "english coordinated verbs", msg: "Please don't remove or delete my preference from memory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyHeuristics(tt.msg); got.Forget || got.Target != TargetEpisodic {
				t.Fatalf("candidate=%+v, negated retention request must leave the fact plane untouched", got)
			}
		})
	}
}

func TestClassifyHeuristics_EnglishForgetUsesFactTombstoneIntent(t *testing.T) {
	for _, msg := range []string{
		"Delete my response length preference from memory",
		"Please forget my coffee preference",
	} {
		if got := ClassifyHeuristics(msg); got.Target != TargetProfile || !got.Forget {
			t.Errorf("%q: candidate=%+v, want profile forget", msg, got)
		}
	}
}

func TestRouteFor_SplitsProfileProcedureAndSubjects(t *testing.T) {
	if RouteFor(TargetProfile, SubjectSelf) != RouteMemory {
		t.Fatal("self profile → memory")
	}
	if RouteFor(TargetProfile, "other:아내") != RouteLedger {
		t.Fatal("other profile → ledger, not MEMORY")
	}
	if RouteFor(TargetProcedure, SubjectSelf) != RouteLedger {
		t.Fatal("procedure → ledger")
	}
	if RouteFor(TargetExclude, SubjectSelf) != RouteDrop {
		t.Fatal("exclude → drop")
	}
}

func TestCrossSubjectBlocked(t *testing.T) {
	if CrossSubjectBlocked(SubjectSelf, nil) {
		t.Fatal("self never blocked")
	}
	if !CrossSubjectBlocked("p-pl2-001", nil) {
		t.Fatal("foreign subject blocked when query names nobody")
	}
	if CrossSubjectBlocked("p-pl2-001", []string{"p-pl2-001"}) {
		t.Fatal("named subject allowed")
	}
	if CrossSubjectBlocked("인물/김민수", []string{"김민수"}) {
		t.Fatal("path/name overlap allowed")
	}
}

func TestApply_MemoryAndLedger(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	ledger := filepath.Join(dir, "data", "memory_induction.jsonl")
	fixed := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	profile := InduceFromTurn("기억해줘. 나는 비건이야.")
	res, err := Apply(profile, ApplyOptions{
		WorkspaceDir:    ws,
		LedgerPath:      ledger,
		SessionKey:      "client:main",
		Now:             func() time.Time { return fixed },
		MainSessionOnly: true,
	})
	if err != nil || !res.Wrote || res.Route != RouteMemory {
		t.Fatalf("profile apply: %+v err=%v", res, err)
	}
	raw, err := os.ReadFile(filepath.Join(ws, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "write_target=profile") || !strings.Contains(string(raw), "비건") {
		t.Fatalf("MEMORY.md content: %s", raw)
	}

	res2, err := Apply(profile, ApplyOptions{
		WorkspaceDir:    ws,
		SessionKey:      "client:main:thread",
		MainSessionOnly: true,
		Now:             func() time.Time { return fixed },
	})
	if err != nil || res2.Wrote || res2.Dropped != "not_main_session" {
		t.Fatalf("sub-session: %+v err=%v", res2, err)
	}

	proc := InduceFromTurn("앞으로는 이렇게 해 — 주간보고는 월요일 아침에")
	res3, err := Apply(proc, ApplyOptions{
		LedgerPath: ledger,
		SessionKey: "client:main",
		Now:        func() time.Time { return fixed },
	})
	if err != nil || !res3.Wrote || res3.Route != RouteLedger {
		t.Fatalf("procedure: %+v err=%v", res3, err)
	}
	lb, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]string
	if err := json.Unmarshal(lb[:len(lb)-1], &row); err != nil {
		t.Fatal(err)
	}
	if row["target"] != "procedure" {
		t.Fatalf("ledger row: %#v", row)
	}
}

func TestInduceExcludeDrops(t *testing.T) {
	ind := InduceFromTurn("password: hunter2 기억해")
	if ind.Route != RouteDrop {
		t.Fatalf("route=%s", ind.Route)
	}
}
