package classification

// FAKE data only — see classifier_test.go.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFile_MissingFileUsesDefaults(t *testing.T) {
	// A path that doesn't exist must NOT error — it falls back to the keyword
	// defaults so a fresh install still classifies by keyword.
	rules, err := LoadFromFile(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("missing file: unexpected error: %v", err)
	}
	if len(rules.PersonToLane) != 0 {
		t.Errorf("missing file: PersonToLane should be empty, got %d entries", len(rules.PersonToLane))
	}
	// Keyword defaults present (e.g. 루프탑 → team2 ships in code).
	if rules.KeywordToLane["루프탑"] != LaneTeam2 {
		t.Errorf("missing file: default keyword 루프탑 = %q, want team2", rules.KeywordToLane["루프탑"])
	}
	// And the defaults actually classify.
	if lane, _ := rules.Classify(Signals{Text: "루프탑 점검"}); lane != LaneTeam2 {
		t.Errorf("missing file: keyword classify lane = %q, want team2", lane)
	}
}

func TestLoadFromFile_MergesOperatorRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "classification_rules.json")
	// Operator JSON with fake names. personToLane uses a display name with an
	// honorific to prove normalization at load time.
	content := `{
	  "personToLane": {"홍길동 부장": "team1", "최지우": "namdo"},
	  "companyToLane": {"가나에너지": "namdo"},
	  "keywordToLane": {"태양광": "team3"}
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Person normalized at load: lookup key is "홍길동" (honorific peeled).
	if rules.PersonToLane["홍길동"] != LaneTeam1 {
		t.Errorf("merged person 홍길동 = %q, want team1", rules.PersonToLane["홍길동"])
	}
	if rules.PersonToLane["최지우"] != LaneNamdo {
		t.Errorf("merged person 최지우 = %q, want namdo", rules.PersonToLane["최지우"])
	}
	// Company normalized (lowercased/space-stripped — Korean unchanged here).
	if rules.CompanyToLane["가나에너지"] != LaneNamdo {
		t.Errorf("merged company = %q, want namdo", rules.CompanyToLane["가나에너지"])
	}
	// Operator keyword ADDED on top of the in-code defaults; defaults survive.
	if rules.KeywordToLane["태양광"] != LaneTeam3 {
		t.Errorf("merged keyword 태양광 = %q, want team3", rules.KeywordToLane["태양광"])
	}
	if rules.KeywordToLane["인허가"] != LaneTeam1 {
		t.Errorf("default keyword 인허가 lost after merge = %q, want team1", rules.KeywordToLane["인허가"])
	}

	// End-to-end: the merged person rule classifies a meeting attendee.
	if lane, conf := rules.Classify(Signals{People: []string{"홍길동 상무"}}); lane != LaneTeam1 || conf != ConfStrong {
		t.Errorf("merged classify: got (%q, %d), want (team1, ConfStrong)", lane, conf)
	}
}

func TestLoadFromFile_OperatorKeywordOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "classification_rules.json")
	// Reassign a default keyword (루프탑 ships as team2) to a different lane.
	if err := os.WriteFile(path, []byte(`{"keywordToLane": {"루프탑": "team3"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if rules.KeywordToLane["루프탑"] != LaneTeam3 {
		t.Errorf("override: 루프탑 = %q, want team3 (operator should win)", rules.KeywordToLane["루프탑"])
	}
}

func TestLoadFromFile_InvalidLanesDropped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "classification_rules.json")
	// "team99" is not a real lane → that entry must be dropped, not routed.
	content := `{"personToLane": {"홍길동": "team99", "최지우": "namdo"}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := rules.PersonToLane["홍길동"]; ok {
		t.Error("invalid lane: 홍길동→team99 should have been dropped")
	}
	if rules.PersonToLane["최지우"] != LaneNamdo {
		t.Error("invalid lane: valid sibling 최지우→namdo should survive")
	}
}

func TestLoadFromFile_CorruptJSONErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "classification_rules.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A present-but-corrupt file is a real error (surfaces a typo) — unlike a
	// missing file, which is fine.
	if _, err := LoadFromFile(path); err == nil {
		t.Fatal("corrupt JSON: expected an error, got nil")
	}
}

func TestExampleTemplateIsValidAndFake(t *testing.T) {
	// The shipped example template must parse cleanly (it's documentation the
	// operator copies) and must classify with its fake entries.
	rules, err := LoadFromFile("classification_rules.example.json")
	if err != nil {
		t.Fatalf("example template failed to load: %v", err)
	}
	// A fake person from the template classifies (proves the template's shape is
	// the shape the loader expects).
	if lane, _ := rules.Classify(Signals{People: []string{"홍길동"}}); lane != LaneTeam1 {
		t.Errorf("example template: 홍길동 lane = %q, want team1", lane)
	}
}
