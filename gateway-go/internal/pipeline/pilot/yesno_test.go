package pilot

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// writePilotCompletion answers a non-streaming harness request. Modes:
//
//	prob:<p>       first token YES (p >= 0.5) or NO, top alternatives YES=p·0.98, NO=(1-p)·0.98, "**"=0.02
//	nolog:<token>  the token with no logprobs (OpenRouter's hosted models)
//	lowmass        first token "**"; YES and NO hold only 0.1 between them
//	(default)      "reply:<model>" with no logprobs
func writePilotCompletion(w http.ResponseWriter, model, mode string) {
	type alt struct {
		Token   string  `json:"token"`
		Logprob float64 `json:"logprob"`
	}
	content := "reply:" + model
	var top []alt
	switch {
	case strings.HasPrefix(mode, "prob:"):
		p, _ := strconv.ParseFloat(strings.TrimPrefix(mode, "prob:"), 64)
		content = "NO"
		if p >= 0.5 {
			content = "YES"
		}
		top = []alt{{"YES", math.Log(p * 0.98)}, {"NO", math.Log((1 - p) * 0.98)}, {"**", math.Log(0.02)}}
	case strings.HasPrefix(mode, "nolog:"):
		content = strings.TrimPrefix(mode, "nolog:")
	case mode == "lowmass":
		content = "**"
		top = []alt{{"**", math.Log(0.9)}, {"YES", math.Log(0.06)}, {"NO", math.Log(0.04)}}
	}
	choice := map[string]any{"message": map[string]any{"content": content}, "finish_reason": "length"}
	if top != nil {
		choice["logprobs"] = map[string]any{"content": []any{map[string]any{"token": content, "top_logprobs": top}}}
	}
	w.Header().Set("Content-Type", "application/json")
	data, _ := json.Marshal(map[string]any{
		"choices": []any{choice},
		"usage":   map[string]any{"prompt_tokens": 12, "completion_tokens": 1},
	})
	fmt.Fprint(w, string(data))
}

func TestCallRoleYesNoReadsProbabilityFromFirstToken(t *testing.T) {
	resetPilotHarness()
	setPilotMode("tiny", "prob:0.8")
	got, err := CallTinyYesNo(context.Background(), "system", "user")
	if err != nil {
		t.Fatalf("CallTinyYesNo: %v", err)
	}
	if !got.HasProb || math.Abs(got.PYes-0.8) > 1e-9 || got.Token != "YES" || got.Model != "tiny" {
		t.Fatalf("verdict = %#v, want P(YES)=0.8 from the tiny model", got)
	}
	reqs := pilotRequests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	req := reqs[0]
	if req["max_tokens"] != float64(1) || req["stream"] != false || req["logprobs"] != true ||
		req["top_logprobs"] != float64(yesNoTopN) || req["temperature"] != float64(0) {
		t.Fatalf("request = %#v, want a one-token, non-streaming, temperature-0 logprobs call", req)
	}
	if timeout, ok := req["timeout"].(float64); !ok || timeout <= 1 {
		t.Fatalf("server timeout = %#v, want the pilot deadline forwarded", req["timeout"])
	}
}

// Without logprobs (or with too little YES/NO mass to trust the ratio) the
// verdict is the token alone — the caller parses it the way it parsed text.
func TestCallRoleYesNoWithoutUsableLogprobsReturnsToken(t *testing.T) {
	for _, tc := range []struct{ mode, token string }{
		{"nolog:NO", "NO"},
		{"lowmass", "**"},
	} {
		resetPilotHarness()
		setPilotMode("tiny", tc.mode)
		got, err := CallTinyYesNo(context.Background(), "system", "user")
		if err != nil || got.HasProb || got.Token != tc.token {
			t.Fatalf("%s: verdict = %#v/%v, want token %q without a probability", tc.mode, got, err, tc.token)
		}
	}
}

func TestCallRoleYesNoWalksHelperFallbacks(t *testing.T) {
	resetPilotHarness()
	setPilotMode("tiny", "http-error")
	setPilotMode("light", "prob:0.3")
	got, err := CallRoleYesNo(context.Background(), modelrole.RoleTiny, "system", "user")
	if err != nil || got.Model != "light" || !got.HasProb || math.Abs(got.PYes-0.3) > 1e-9 {
		t.Fatalf("verdict = %#v/%v, want the lightweight fallback's P(YES)=0.3", got, err)
	}
	var models []any
	for _, req := range pilotRequests() {
		models = append(models, req["model"])
	}
	if len(models) != 2 || models[0] != "tiny" || models[1] != "light" {
		t.Fatalf("models tried = %#v, want tiny then light", models)
	}

	resetPilotHarness()
	for _, m := range []string{"tiny", "light", "fallback"} {
		setPilotMode(m, "http-error")
	}
	if got, err := CallRoleYesNo(context.Background(), modelrole.RoleTiny, "system", "user"); err == nil || !strings.Contains(err.Error(), "all models failed") {
		t.Fatalf("all failed = %#v/%v, want an error the caller can fail open on", got, err)
	}
}

func TestYesNoProbability(t *testing.T) {
	for _, tc := range []struct {
		name   string
		top    []llm.TokenProb
		want   float64
		wantOK bool
	}{
		{"yes and no split", []llm.TokenProb{{Token: "YES", Logprob: math.Log(0.6)}, {Token: "NO", Logprob: math.Log(0.3)}}, 0.6 / 0.9, true},
		{"case and space variants fold", []llm.TokenProb{{Token: " yes", Logprob: math.Log(0.5)}, {Token: "No", Logprob: math.Log(0.3)}, {Token: "YES", Logprob: math.Log(0.1)}}, 0.6 / 0.9, true},
		{"too little yes/no mass", []llm.TokenProb{{Token: "**", Logprob: math.Log(0.7)}, {Token: "YES", Logprob: math.Log(0.2)}, {Token: "NO", Logprob: math.Log(0.1)}}, 0, false},
		{"no alternatives", nil, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := yesNoProbability(tc.top)
			if ok != tc.wantOK || math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("yesNoProbability = %v/%v, want %v/%v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
