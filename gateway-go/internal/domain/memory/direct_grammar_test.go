package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The catalog is compiled into the binary, so a malformed edit must fail here
// rather than at the first user turn that would have written a fact.
func TestDirectGrammarCatalogIsWellFormed(t *testing.T) {
	if len(factAxes) == 0 {
		t.Fatal("catalog is empty")
	}
	for _, axis := range factAxes {
		if !strings.Contains(axis.Key, ".") {
			t.Errorf("axis key %q is not a namespaced fact key", axis.Key)
		}
		if axis.classifyRE == nil {
			t.Errorf("axis %q has no compiled classify pattern", axis.Key)
		}
		if axis.Kind != "preference" && axis.Kind != "identity" {
			t.Errorf("axis %q kind = %q", axis.Key, axis.Kind)
		}
	}
}

func TestLoadDirectGrammarRejectsBrokenCatalogs(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"unsupported version", `{"schemaVersion":99,"axes":[]}`, "unsupported schema version"},
		{"empty", `{"schemaVersion":1,"axes":[]}`, "catalog is empty"},
		{"missing key", `{"schemaVersion":1,"axes":[{"kind":"preference","classify":"x"}]}`, "no key"},
		{
			"duplicate key",
			`{"schemaVersion":1,"axes":[{"key":"a.b","kind":"preference","classify":"x"},{"key":"a.b","kind":"preference","classify":"y"}]}`,
			"duplicate axis key",
		},
		{"bad kind", `{"schemaVersion":1,"axes":[{"key":"a.b","kind":"guess","classify":"x"}]}`, "unsupported kind"},
		{"missing classify", `{"schemaVersion":1,"axes":[{"key":"a.b","kind":"preference"}]}`, "no classify pattern"},
		{"bad pattern", `{"schemaVersion":1,"axes":[{"key":"a.b","kind":"preference","classify":"("}]}`, "classify pattern"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadDirectGrammar([]byte(tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

// Externalizing the table must not change what binds. Each row is a phrasing
// the shipped grammar was already responsible for.
func TestDirectGrammarBindsEachPublishedAxis(t *testing.T) {
	cases := []struct {
		message string
		key     string
		kind    string
		forget  bool
	}{
		{message: "기억해줘. 나는 답변을 짧게 원해", key: "communication.response_length", kind: "preference"},
		{message: "remember: i prefer short answers", key: "communication.response_length", kind: "preference"},
		{message: "from now on answer in korean", key: "communication.language", kind: "preference"},
		{message: "from now on use bullet lists", key: "communication.format", kind: "preference"},
		{message: "from now on put the answer first", key: "communication.answer_first", kind: "preference"},
		{message: "from now on give me progress updates", key: "communication.progress_updates", kind: "preference"},
		{message: "from now on list vat separately", key: "wiki.amount_vat_policy", kind: "preference"},
		{message: "from now on call me 대표님", key: "identity.address", kind: "identity"},
		{message: "remember i am vegan", key: "diet.vegan", kind: "identity"},
		{message: "remember i am allergic to peanuts", key: "health.allergy", kind: "identity"},
		{message: "remember my long-term goal is to ship deneb", key: "goals.long_term", kind: "preference"},
		{message: "내 답변 길이 선호는 기억에서 지워줘", key: "communication.response_length", kind: "preference", forget: true},
		{message: "내 호칭 정보는 지워줘", key: "identity.address", kind: "identity", forget: true},
		{message: "내 부가세 정책은 지워줘", key: "wiki.amount_vat_policy", kind: "preference", forget: true},
	}
	for _, tc := range cases {
		t.Run(tc.message, func(t *testing.T) {
			candidate := ClassifyHeuristics(tc.message)
			if candidate.Target != TargetProfile {
				t.Fatalf("target = %v", candidate.Target)
			}
			if candidate.FactKey != tc.key {
				t.Errorf("fact key = %q, want %q", candidate.FactKey, tc.key)
			}
			if candidate.FactKind != tc.kind {
				t.Errorf("fact kind = %q, want %q", candidate.FactKind, tc.kind)
			}
			if candidate.Forget != tc.forget {
				t.Errorf("forget = %v, want %v", candidate.Forget, tc.forget)
			}
			if !HasDirectProfileMutationIntent(tc.message) {
				t.Errorf("published axis phrasing must be a direct mutation intent")
			}
		})
	}
}

func TestFactAxisCueRequiresEveryAllTerm(t *testing.T) {
	var cue factAxisCue
	if err := json.Unmarshal([]byte(`{"all":["답변"],"any":["길이","분량"]}`), &cue); err != nil {
		t.Fatal(err)
	}
	if !cue.matches("내 답변 길이 기록") {
		t.Error("all+any cue should match")
	}
	if cue.matches("답변 형식") {
		t.Error("cue matched without any term")
	}
	if cue.matches("분량 조절") {
		t.Error("cue matched without the required all term")
	}

	var sugar factAxisCue
	if err := json.Unmarshal([]byte(`"알레르기"`), &sugar); err != nil {
		t.Fatal(err)
	}
	if !sugar.matches("내 알레르기 정보") || sugar.matches("무관한 문장") {
		t.Errorf("string sugar cue = %+v", sugar)
	}
}

func TestDirectMemoryMissRecordsOnlyUnboundCommands(t *testing.T) {
	const boundMessage = "remember: i prefer short answers"
	bound := InduceFromTurn(boundMessage)
	if bound.Route != RouteMemory {
		t.Fatalf("published phrasing route = %v", bound.Route)
	}
	if _, found := DirectMemoryMissFor(boundMessage, bound); found {
		t.Fatal("a bound command must not be recorded as a miss")
	}

	const unbound = "기억해줘. 나 회의는 화요일 오전에만 잡아줘."
	ind := InduceFromTurn(unbound)
	miss, found := DirectMemoryMissFor(unbound, ind)
	if !found {
		t.Fatalf("command-shaped turn was not captured: %+v", ind)
	}
	if miss.Lead != "remember" || miss.Message == "" {
		t.Fatalf("miss = %+v", miss)
	}

	chat := InduceFromTurn("오늘 날씨 어때?")
	if _, found := DirectMemoryMissFor("오늘 날씨 어때?", chat); found {
		t.Fatal("ordinary conversation must not be captured")
	}
}

func TestRecordDirectMemoryMissAppendsAndStaysBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "misses.jsonl")
	for i := range 3 {
		if err := RecordDirectMemoryMiss(path, DirectMemoryMiss{
			Lead: "remember", Message: "기억해줘. 사례 " + string(rune('a'+i)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(rows) != 3 {
		t.Fatalf("rows = %d", len(rows))
	}
	var decoded DirectMemoryMiss
	if err := json.Unmarshal([]byte(rows[0]), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.AtMs == 0 || decoded.Lead != "remember" {
		t.Fatalf("row = %+v", decoded)
	}

	// A ledger at its cap stops accepting rows instead of growing without bound.
	if err := os.WriteFile(path, make([]byte, directMemoryMissMaxBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordDirectMemoryMiss(path, DirectMemoryMiss{Lead: "remember", Message: "새 사례"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != directMemoryMissMaxBytes {
		t.Fatalf("capped ledger grew to %d", info.Size())
	}
}
