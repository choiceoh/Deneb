package briefcase

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestChatHarnessContinuesWithSameDenebSession(t *testing.T) {
	pack := writeHarnessCase(t)
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := ioReadAll(r)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		requests = append(requests, string(body))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if len(requests) == 1 {
			fmt.Fprint(w, textSSE("I found a draft but have not verified it."))
			return
		}
		fmt.Fprint(w, textSSE("I verified the revision: the approved budget is 120."))
	}))
	defer server.Close()

	root, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	harness, err := NewChatHarness(ChatHarnessConfig{
		Pack: pack, Root: root, Client: llm.NewClient(server.URL, "test-key"), Model: "test-model", Arm: ArmRawPrimary,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()

	if _, err := harness.Continue(t.Context(), "followup-1", "Please verify the result."); !errors.Is(err, ErrHarnessNotRun) {
		t.Fatalf("continue before initial run error = %v", err)
	}
	initial, err := harness.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.Episodes) != 1 || initial.Episodes[0].Phase != "timeline" || initial.Episodes[0].InputSHA256 == "" {
		t.Fatalf("initial episodes = %+v", initial.Episodes)
	}
	continued, err := harness.Continue(t.Context(), "followup-1", "Please verify the result.")
	if err != nil {
		t.Fatal(err)
	}
	if len(continued.Episodes) != 2 {
		t.Fatalf("continued episodes = %+v", continued.Episodes)
	}
	followUp := continued.Episodes[1]
	if followUp.Phase != "follow-up" || followUp.Cycle != 1 || followUp.InputSHA256 == "" || !strings.Contains(followUp.Text, "120") {
		t.Fatalf("follow-up episode = %+v", followUp)
	}
	if len(requests) != 2 || !strings.Contains(requests[1], "I found a draft") || !strings.Contains(requests[1], "Please verify the result") {
		t.Fatalf("follow-up did not continue the transcript: %v", requests)
	}
	var second struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(requests[1]), &second); err != nil {
		t.Fatalf("decode second model request: %v", err)
	}
	roles := make([]string, 0, len(second.Messages))
	for _, message := range second.Messages {
		roles = append(roles, message.Role)
	}
	if strings.Join(roles, ",") != "system,user,assistant,user" {
		t.Fatalf("follow-up role history = %v, want system,user,assistant,user", roles)
	}

	// Returned results are detached snapshots and cannot mutate harness state.
	continued.Episodes[0].Text = "tampered"
	snapshot, err := harness.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Episodes[0].Text == "tampered" {
		t.Fatal("caller mutated harness result through returned snapshot")
	}
	if _, err := harness.Run(t.Context()); !errors.Is(err, ErrHarnessAlreadyRun) {
		t.Fatalf("second initial run error = %v", err)
	}
	if _, err := harness.Continue(t.Context(), "followup-1", "again"); err == nil || !strings.Contains(err.Error(), "duplicate episode") {
		t.Fatalf("duplicate follow-up error = %v", err)
	}
}
