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
