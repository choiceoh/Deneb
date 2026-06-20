package handlerminiapp

// FAKE data only — invented names. The real chart lives in the operator's
// {stateDir}/org.json, not the repo (privacy invariant from the org/
// classification packages).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// fakeOrgDeps wires the handler to a temp file: Load reads it (missing → empty
// tree), SavePath points at it. Mirrors production (org.Load / ResolvePath) but
// scoped to t.TempDir() so no real state dir is touched.
func fakeOrgDeps(t *testing.T) (OrgDeps, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "org.json")
	deps := OrgDeps{
		Load:     func() (org.OrgTree, error) { return org.LoadFromFile(path) },
		SavePath: func() string { return path },
	}
	return deps, path
}

// validWireTree is a small valid chart in wire shape (one lane team under a
// group root). Fake names throughout; the member carries a 직급/직책 to exercise
// the structured member wire shape.
func validWireTree() OrgTreeOut {
	return OrgTreeOut{Nodes: []OrgNodeOut{
		{ID: "g", Name: "예시그룹", Type: org.NodeTypeGroup},
		{ID: "t1", Name: "1팀", Type: org.NodeTypeTeam, ParentID: "g", Lane: "team1",
			Members:  []MemberOut{{Name: "김철수", Rank: org.RankExecVP, Position: org.PositionTeamLead}},
			Keywords: []string{"인허가"}},
	}}
}

func TestOrgMethods_NilDepsReturnsNil(t *testing.T) {
	if got := OrgMethods(OrgDeps{}); got != nil {
		t.Fatalf("OrgMethods(nil) = %v, want nil", got)
	}
	// Partial wiring is also unregistered (both fields required).
	if got := OrgMethods(OrgDeps{Load: func() (org.OrgTree, error) { return org.OrgTree{}, nil }}); got != nil {
		t.Fatalf("OrgMethods(load-only) = %v, want nil", got)
	}
}

func TestOrgMethods_Registers(t *testing.T) {
	deps, _ := fakeOrgDeps(t)
	m := OrgMethods(deps)
	for _, name := range []string{"miniapp.org.get", "miniapp.org.save"} {
		if _, ok := m[name]; !ok {
			t.Fatalf("%s not registered", name)
		}
	}
}

func TestOrgGet_RequiresAuth(t *testing.T) {
	deps, _ := fakeOrgDeps(t)
	resp := orgGet(deps)(context.Background(), reqWith(t, "miniapp.org.get", nil))
	if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("expected unauthorized, got OK=%v code=%v", resp.OK, resp.Error)
	}
}

func TestOrgSave_RequiresAuth(t *testing.T) {
	deps, _ := fakeOrgDeps(t)
	resp := orgSave(deps)(context.Background(), reqWith(t, "miniapp.org.save", validWireTree()))
	if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("expected unauthorized, got OK=%v code=%v", resp.OK, resp.Error)
	}
}

func TestOrgGet_MissingFileReturnsEmpty(t *testing.T) {
	// No file yet → empty tree, not an error (editor opens blank).
	deps, _ := fakeOrgDeps(t)
	resp := orgGet(deps)(authedCtx(), reqWith(t, "miniapp.org.get", nil))
	var got OrgTreeOut
	decode(t, resp, &got)
	if len(got.Nodes) != 0 {
		t.Fatalf("missing file: got %d nodes, want 0", len(got.Nodes))
	}
}

func TestOrgGet_LoadErrorIsUnavailable(t *testing.T) {
	deps := OrgDeps{
		Load:     func() (org.OrgTree, error) { return org.OrgTree{}, errors.New("disk on fire") },
		SavePath: func() string { return "" },
	}
	resp := orgGet(deps)(authedCtx(), reqWith(t, "miniapp.org.get", nil))
	if resp.OK || resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("load error: got OK=%v code=%v, want UNAVAILABLE", resp.OK, resp.Error)
	}
}

func TestOrgSave_ValidWritesAndAcks(t *testing.T) {
	deps, path := fakeOrgDeps(t)
	resp := orgSave(deps)(authedCtx(), reqWith(t, "miniapp.org.save", validWireTree()))
	var ack OrgSaveOut
	decode(t, resp, &ack)
	if !ack.Saved || ack.NodeCount != 2 || !ack.HasLanes {
		t.Fatalf("ack = %+v, want saved/2 nodes/hasLanes", ack)
	}
	// File actually written and parses back as the same chart.
	tree, err := org.LoadFromFile(path)
	if err != nil {
		t.Fatalf("reload saved file: %v", err)
	}
	if len(tree.Nodes) != 2 || !tree.HasLanes() {
		t.Fatalf("saved tree = %+v, want 2 nodes with lanes", tree)
	}
	// And it derives the expected rule (the save is what drives the dashboard).
	if tree.DeriveRules().PersonToLane["김철수"] != "team1" {
		t.Fatalf("derived rule lost: 김철수 not → team1")
	}
}

func TestOrgSave_RoundTripsThroughGet(t *testing.T) {
	deps, _ := fakeOrgDeps(t)
	if r := orgSave(deps)(authedCtx(), reqWith(t, "miniapp.org.save", validWireTree())); !r.OK {
		t.Fatalf("save failed: %v", r.Error)
	}
	resp := orgGet(deps)(authedCtx(), reqWith(t, "miniapp.org.get", nil))
	var got OrgTreeOut
	decode(t, resp, &got)
	if len(got.Nodes) != 2 || got.Nodes[1].Lane != "team1" ||
		got.Nodes[1].Members[0].Name != "김철수" ||
		got.Nodes[1].Members[0].Rank != org.RankExecVP ||
		got.Nodes[1].Members[0].Position != org.PositionTeamLead {
		t.Fatalf("round-trip tree = %+v, want the saved chart (with rank/position)", got)
	}
}

func TestOrgSave_InvalidRejectedAndFileUntouched(t *testing.T) {
	deps, path := fakeOrgDeps(t)
	// Seed a good file first so we can prove an invalid save does NOT overwrite it.
	if r := orgSave(deps)(authedCtx(), reqWith(t, "miniapp.org.save", validWireTree())); !r.OK {
		t.Fatalf("seed save failed: %v", r.Error)
	}
	before, _ := os.ReadFile(path)

	// Invalid: references a missing parent → must be rejected with VALIDATION.
	bad := OrgTreeOut{Nodes: []OrgNodeOut{
		{ID: "x", Name: "X", Type: org.NodeTypeTeam, ParentID: "ghost"},
	}}
	resp := orgSave(deps)(authedCtx(), reqWith(t, "miniapp.org.save", bad))
	if resp.OK || resp.Error.Code != protocol.ErrValidationFailed {
		t.Fatalf("invalid save: got OK=%v code=%v, want VALIDATION_FAILED", resp.OK, resp.Error)
	}
	// File unchanged (the existing good chart survives).
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid save overwrote the existing file")
	}
}

func TestOrgSave_DuplicateLaneRejected(t *testing.T) {
	deps, _ := fakeOrgDeps(t)
	dup := OrgTreeOut{Nodes: []OrgNodeOut{
		{ID: "a", Name: "A", Type: org.NodeTypeTeam, Lane: "x"},
		{ID: "b", Name: "B", Type: org.NodeTypeTeam, Lane: "x"},
	}}
	resp := orgSave(deps)(authedCtx(), reqWith(t, "miniapp.org.save", dup))
	if resp.OK || resp.Error.Code != protocol.ErrValidationFailed {
		t.Fatalf("duplicate lane: got OK=%v code=%v, want VALIDATION_FAILED", resp.OK, resp.Error)
	}
}

func TestOrgSave_TooManyNodesRejected(t *testing.T) {
	deps, _ := fakeOrgDeps(t)
	nodes := make([]OrgNodeOut, maxOrgNodes+1)
	for i := range nodes {
		nodes[i] = OrgNodeOut{ID: "n" + itoa(i), Name: "N", Type: org.NodeTypeTeam}
	}
	resp := orgSave(deps)(authedCtx(), reqWith(t, "miniapp.org.save", OrgTreeOut{Nodes: nodes}))
	if resp.OK || resp.Error.Code != protocol.ErrValidationFailed {
		t.Fatalf("too many nodes: got OK=%v code=%v, want VALIDATION_FAILED", resp.OK, resp.Error)
	}
}

func TestOrgSave_EmptyTreeClearsChart(t *testing.T) {
	// Saving an empty node list is valid (it clears the chart) — the dashboard
	// then falls back to legacy lanes. Confirms an empty save round-trips.
	deps, _ := fakeOrgDeps(t)
	resp := orgSave(deps)(authedCtx(), reqWith(t, "miniapp.org.save", OrgTreeOut{Nodes: []OrgNodeOut{}}))
	var ack OrgSaveOut
	decode(t, resp, &ack)
	if !ack.Saved || ack.NodeCount != 0 || ack.HasLanes {
		t.Fatalf("empty save ack = %+v, want saved/0/no-lanes", ack)
	}
}
