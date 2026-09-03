package skillsrpc

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func callSelfImprovementList(t *testing.T, h rpcutil.HandlerFunc, params string) SelfImprovementCodingListResponse {
	t.Helper()
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{
		ID:     "1",
		Method: "miniapp.self_improvement_coding.list",
		Params: []byte(params),
	})
	return decodeSkillsPayload[SelfImprovementCodingListResponse](t, resp)
}

// stubTranslator marks anything it is given, so a test can see exactly which
// fields reached the translator and which never did.
func stubTranslator(t *testing.T) *[]string {
	t.Helper()
	seen := &[]string{}
	original := translateProseFn
	t.Cleanup(func() { translateProseFn = original })
	translateProseFn = func(_ context.Context, texts []string) []string {
		out := make([]string, len(texts))
		for i, s := range texts {
			if strings.TrimSpace(s) == "" {
				out[i] = s
				continue
			}
			*seen = append(*seen, s)
			out[i] = "KO:" + s
		}
		return out
	}
	return seen
}

func sampleRecords() []genesis.SelfCorrectionCandidateRecord {
	return []genesis.SelfCorrectionCandidateRecord{{
		ID:             "sc-1",
		Status:         "proposed",
		Scope:          "code",
		SessionKey:     "cron:morning-letter:1780959600105",
		SkillName:      "email-analysis",
		Title:          "Promote rejected evolve into held-out validation",
		Candidate:      "Rejected evolve should become a validation case",
		Evidence:       "observe.behavior 7d vs 30d baseline: mail_archive calls=113 avgMs=750",
		Reason:         "agentlog tool-latency signal",
		ProposedChange: "Review the rejected candidate and add a held-out case",
		Risk:           "Do not auto-apply the rejected body",
		ReviewNote:     "Guardrail working as designed",
	}}
}

// Evidence is the raw proof an operator judges the candidate by — telemetry
// lines and session keys. Machine-translating a measurement is how a review
// screen starts lying about numbers, so it must never reach the translator.
// Identifiers (id, sessionKey, skillName, status) are not prose at all.
func TestTranslateSelfCorrectionProseLeavesEvidenceAndIdentifiersAlone(t *testing.T) {
	seen := stubTranslator(t)
	candidates := []SelfCorrectionCandidate{selfCorrectionCandidate(sampleRecords()[0])}

	translateSelfCorrectionProse(context.Background(), candidates)

	got := candidates[0]
	for name, value := range map[string]string{
		"Title":          got.Title,
		"Candidate":      got.Candidate,
		"Reason":         got.Reason,
		"ProposedChange": got.ProposedChange,
		"Risk":           got.Risk,
		"ReviewNote":     got.ReviewNote,
	} {
		if !strings.HasPrefix(value, "KO:") {
			t.Errorf("%s was not translated: %q", name, value)
		}
	}
	if strings.HasPrefix(got.Evidence, "KO:") {
		t.Error("evidence reached the translator — it is proof text and must stay verbatim")
	}
	if got.SessionKey != "cron:morning-letter:1780959600105" || got.SkillName != "email-analysis" || got.ID != "sc-1" {
		t.Errorf("an identifier was rewritten: %+v", got)
	}
	for _, s := range *seen {
		if strings.Contains(s, "observe.behavior") || strings.Contains(s, "cron:morning-letter") {
			t.Errorf("machine text was sent to the translator: %q", s)
		}
	}
}

// The dispatch selector feeds a coding agent its instructions. Translating those
// would silently rewrite what the agent is told to do, so that path must stay
// byte-identical no matter what the caller asks for.
func TestSelfImprovementCodingListNeverTranslatesTheDispatchPath(t *testing.T) {
	seen := stubTranslator(t)
	recs := sampleRecords()
	deps := SelfImprovementCodingDeps{
		RecentCandidates: func(string, int) ([]genesis.SelfCorrectionCandidateRecord, error) { return recs, nil },
		NextDispatchCandidate: func([]string) (genesis.SelfCorrectionCandidateRecord, bool, error) {
			return recs[0], true, nil
		},
	}
	handler := selfImprovementCodingList(deps)

	resp := callSelfImprovementList(t, handler, `{"dispatchableOnly":true,"translate":true}`)
	if len(resp.Candidates) != 1 {
		t.Fatalf("candidates=%d", len(resp.Candidates))
	}
	if resp.Candidates[0].ProposedChange != recs[0].ProposedChange {
		t.Fatalf("dispatch candidate was translated: %q", resp.Candidates[0].ProposedChange)
	}
	if len(*seen) != 0 {
		t.Fatalf("the dispatch path called the translator: %v", *seen)
	}
}

// Scripts that do not ask keep the exact bytes they had before this feature.
func TestSelfImprovementCodingListLeavesProseAloneWithoutTheFlag(t *testing.T) {
	seen := stubTranslator(t)
	recs := sampleRecords()
	deps := SelfImprovementCodingDeps{
		RecentCandidates: func(string, int) ([]genesis.SelfCorrectionCandidateRecord, error) { return recs, nil },
	}
	handler := selfImprovementCodingList(deps)

	resp := callSelfImprovementList(t, handler, `{"status":"all"}`)
	if len(resp.Candidates) != 1 || resp.Candidates[0].Title != recs[0].Title {
		t.Fatalf("title changed without translate:true: %+v", resp.Candidates)
	}
	if len(*seen) != 0 {
		t.Fatalf("translator called without the flag: %v", *seen)
	}

	withFlag := callSelfImprovementList(t, handler, `{"status":"all","translate":true}`)
	if !strings.HasPrefix(withFlag.Candidates[0].Title, "KO:") {
		t.Fatalf("translate:true did not translate: %q", withFlag.Candidates[0].Title)
	}
}
