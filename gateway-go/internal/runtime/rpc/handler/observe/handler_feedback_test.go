package observe

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func feedbackReq(t *testing.T, params any) *protocol.RequestFrame {
	t.Helper()
	req, err := protocol.NewRequestFrame("f-1", "observe.workstation_feedback", params)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestWorkstationFeedbackTally(t *testing.T) {
	dir := t.TempDir()
	h := Methods(Deps{StateDir: func() string { return dir }})["observe.workstation_feedback"]

	for _, kept := range []bool{true, true, false} {
		resp := h(context.Background(), feedbackReq(t, map[string]any{"action": "spotlight", "kept": kept}))
		rpctest.MustOK(t, resp)
	}

	data, err := os.ReadFile(filepath.Join(dir, "cache", "workstation_feedback.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tally struct {
		ByAction map[string]struct {
			Kept    int `json:"kept"`
			Dropped int `json:"dropped"`
		} `json:"byAction"`
		LastAt string `json:"lastAt"`
	}
	if err := json.Unmarshal(data, &tally); err != nil {
		t.Fatal(err)
	}
	got := tally.ByAction["spotlight"]
	if got.Kept != 2 || got.Dropped != 1 || tally.LastAt == "" {
		t.Fatalf("tally = %+v lastAt=%q", got, tally.LastAt)
	}
}

func TestWorkstationFeedbackRejectsBlankAction(t *testing.T) {
	h := Methods(Deps{})["observe.workstation_feedback"]
	resp := h(context.Background(), feedbackReq(t, map[string]any{"kept": true}))
	if resp.OK {
		t.Fatal("want missing-param error for blank action")
	}
}
