package org

// FAKE data only — see org_test.go.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/classification"
)

func TestLoadFromFile_MissingReturnsEmptyTree(t *testing.T) {
	// A missing file is the legitimate "no chart yet" state — empty tree, no err.
	tree, err := LoadFromFile(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file: unexpected error: %v", err)
	}
	if len(tree.Nodes) != 0 || tree.HasLanes() {
		t.Fatalf("missing file: want empty tree, got %+v", tree)
	}
}

func TestLoadFromFile_ValidChart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "org.json")
	writeJSON(t, path, fakeTree())

	tree, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(tree.Nodes) != 5 || !tree.HasLanes() {
		t.Fatalf("loaded tree = %+v, want 5 nodes with lanes", tree)
	}
}

func TestLoadFromFile_CorruptJSONErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "org.json")
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFromFile(path); err == nil {
		t.Fatal("corrupt JSON: expected error, got nil")
	}
}

func TestLoadFromFile_InvalidChartErrors(t *testing.T) {
	// Parses fine but references a missing parent → validation must reject it.
	path := filepath.Join(t.TempDir(), "org.json")
	writeJSON(t, path, OrgTree{Nodes: []OrgNode{
		{ID: "a", Name: "A", Type: NodeTypeTeam, ParentID: "ghost"},
	}})
	if _, err := LoadFromFile(path); err == nil {
		t.Fatal("invalid chart: expected error, got nil")
	}
}

func TestLoadRules_OrgChartWins(t *testing.T) {
	// With a chart present (via env override), LoadRules derives from it and
	// ignores the legacy classification file entirely.
	dir := t.TempDir()
	orgPath := filepath.Join(dir, "org.json")
	writeJSON(t, orgPath, fakeTree())
	t.Setenv(orgEnvVar, orgPath)
	// A legacy classification file that, if it leaked through, would assign a
	// DIFFERENT lane — proving the chart took over.
	t.Setenv("DENEB_CLASSIFICATION_RULES", writeClassification(t, dir))

	rules, err := LoadRules()
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	// From the chart: 이몽룡 → team2.
	if rules.PersonToLane["이몽룡"] != classification.Lane("team2") {
		t.Errorf("이몽룡 → %q, want team2 (from chart)", rules.PersonToLane["이몽룡"])
	}
	// The legacy file mapped 이몽룡 → team1; it must NOT be present.
	if rules.PersonToLane["이몽룡"] == classification.LaneTeam1 {
		t.Error("legacy classification file leaked through despite chart present")
	}
}

func TestLoadRules_FallsBackToClassificationWhenNoChart(t *testing.T) {
	// No org.json → LoadRules falls back to the legacy classification loader.
	dir := t.TempDir()
	t.Setenv(orgEnvVar, filepath.Join(dir, "absent-org.json"))
	t.Setenv("DENEB_CLASSIFICATION_RULES", writeClassification(t, dir))

	rules, err := LoadRules()
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	// The legacy file's 이몽룡 → team1 must come through.
	if rules.PersonToLane["이몽룡"] != classification.LaneTeam1 {
		t.Errorf("이몽룡 → %q, want team1 (legacy fallback)", rules.PersonToLane["이몽룡"])
	}
}

func TestLoadLanes_ChartVsFallback(t *testing.T) {
	dir := t.TempDir()
	// Chart present → lanes derived in chart order.
	orgPath := filepath.Join(dir, "org.json")
	writeJSON(t, orgPath, fakeTree())
	t.Setenv(orgEnvVar, orgPath)
	defs, err := LoadLanes()
	if err != nil {
		t.Fatalf("LoadLanes(chart): %v", err)
	}
	if len(defs) != 3 || defs[0].Key != "team1" {
		t.Fatalf("LoadLanes(chart) = %+v, want 3 lanes starting team1", defs)
	}

	// No chart → nil (caller uses legacy hardcoded lanes).
	t.Setenv(orgEnvVar, filepath.Join(dir, "absent.json"))
	defs, err = LoadLanes()
	if err != nil {
		t.Fatalf("LoadLanes(no chart): %v", err)
	}
	if defs != nil {
		t.Fatalf("LoadLanes(no chart) = %+v, want nil", defs)
	}
}

func TestMarshal_ValidatesAndRoundTrips(t *testing.T) {
	tree := fakeTree()
	data, err := tree.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back OrgTree
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if len(back.Nodes) != len(tree.Nodes) {
		t.Fatalf("round-trip lost nodes: %d → %d", len(tree.Nodes), len(back.Nodes))
	}
	// All three member attributes (name + 직급/직책) must survive the round trip;
	// check the t1 lead, whose Rank/Position are distinctive so a dropped field is
	// unambiguous.
	t1Lead := back.Nodes[2].Members[0]
	if t1Lead.Name != "김철수" || t1Lead.Rank != RankExecVP || t1Lead.Position != PositionTeamLead {
		t.Fatalf("round-trip member = %+v, want 김철수/전무/팀장", t1Lead)
	}
	// Marshal of an invalid tree is rejected (the save guard).
	bad := OrgTree{Nodes: []OrgNode{{ID: "a", Name: "A", Type: "nope"}}}
	if _, err := bad.Marshal(); err == nil {
		t.Fatal("Marshal(invalid) expected error, got nil")
	}
}

func TestExampleTemplateIsValidAndFake(t *testing.T) {
	// The shipped example must parse + validate (it's documentation the operator
	// copies) and derive working rules from its fake entries.
	tree, err := LoadFromFile("org.example.json")
	if err != nil {
		t.Fatalf("example template failed to load: %v", err)
	}
	if !tree.HasLanes() {
		t.Fatal("example template defines no lanes")
	}
	rules := tree.DeriveRules()
	// A fake member from the template classifies (성춘향 is the team2 팀장).
	if lane, _ := rules.Classify(classification.Signals{People: []string{"성춘향"}}); lane != classification.Lane("team2") {
		t.Errorf("example: 성춘향 → %q, want team2", lane)
	}
}

// --- helpers --------------------------------------------------------------

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeClassification drops a legacy classification_rules.json (fake names) and
// returns its path. Used to prove org.json takes precedence / the fallback path.
func writeClassification(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "classification_rules.json")
	if err := os.WriteFile(path, []byte(`{"personToLane":{"이몽룡":"team1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
