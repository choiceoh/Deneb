package skilllifecycle

import (
	"encoding/json"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle/leafbind"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestParseUsesSkillVerdict(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantUses   bool
		wantParsed bool
	}{
		{"bare true", `{"uses_skill": true}`, true, true},
		{"bare false", `{"uses_skill": false}`, false, true},
		{"prose wrapped", "Sure! {\"uses_skill\": false} done.", false, true},
		{"code fenced", "```json\n{\"uses_skill\": true}\n```", true, true},
		{"missing field", `{"other": 1}`, false, false},
		{"empty", "", false, false},
		{"not json", "false", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			uses, parsed := parseUsesSkillVerdict(c.raw)
			if uses != c.wantUses || parsed != c.wantParsed {
				t.Fatalf("parseUsesSkillVerdict(%q) = (%v,%v), want (%v,%v)", c.raw, uses, parsed, c.wantUses, c.wantParsed)
			}
		})
	}
}

func TestToolActivityNamesDedupesAndKeepsOrder(t *testing.T) {
	got := toolActivityNames([]leafbind.ToolActivity{
		{Name: "exec"}, {Name: "wiki"}, {Name: "exec"}, {Name: ""}, {Name: "mail_archive"},
	})
	want := []string{"exec", "wiki", "mail_archive"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// With no classifier wired the gate is fail-open: it never drops a case.
func TestSessionExercisesSkillFailsOpenWithoutClassifier(t *testing.T) {
	if !sessionExercisesSkill(slog.Default(), nil, "", "system-health-check", leafbind.SessionContext{Key: "s", AllText: "anything"}) {
		t.Fatal("no classifier wired must fail open (record the case)")
	}
}

func relevanceStubClient(t *testing.T, verdict string) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		content, _ := json.Marshal(verdict)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":`+string(content)+`}}]}`)
	}))
	t.Cleanup(srv.Close)
	return llm.NewClient(srv.URL, "test-key")
}

func TestSessionExercisesSkillSkipsOffTopicKeepsOnTopic(t *testing.T) {
	sctx := leafbind.SessionContext{
		Key:            "client:main",
		AllText:        "user asked about 당진 솔라빌리지 EPC 계약금액 and mail history",
		ToolActivities: []leafbind.ToolActivity{{Name: "mail_archive"}, {Name: "wiki"}},
	}

	// Classifier says the session did NOT exercise the skill → gate skips it.
	if sessionExercisesSkill(slog.Default(), relevanceStubClient(t, `{"uses_skill": false}`), "m", "system-health-check", sctx) {
		t.Error("off-topic session must be skipped when the classifier says uses_skill=false")
	}

	// Classifier says it DID → gate keeps it.
	if !sessionExercisesSkill(slog.Default(), relevanceStubClient(t, `{"uses_skill": true}`), "m", "system-health-check", sctx) {
		t.Error("on-topic session must be recorded when the classifier says uses_skill=true")
	}

	// Unparseable classifier output → fail open (record).
	if !sessionExercisesSkill(slog.Default(), relevanceStubClient(t, `no json here`), "m", "system-health-check", sctx) {
		t.Error("unparseable classifier output must fail open (record the case)")
	}
}
