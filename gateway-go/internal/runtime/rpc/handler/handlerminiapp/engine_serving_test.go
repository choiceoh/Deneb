package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginecontrol"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// fakeControl is the fleet as the gateway asks it.
type fakeControl struct {
	st       enginecontrol.State
	stateErr error
	selected []string
}

func (f *fakeControl) State(context.Context) (enginecontrol.State, error) { return f.st, f.stateErr }
func (f *fakeControl) Target() string                                     { return "choiceoh@srv2" }
func (f *fakeControl) Select(_ context.Context, profile, by, note string) (enginecontrol.State, error) {
	if f.st.ModelOf(profile) == "" {
		return f.st, fmt.Errorf("%w: %q", enginecontrol.ErrUnknownProfile, profile)
	}
	f.selected = append(f.selected, profile+"|"+by+"|"+note)
	f.st.Selected, f.st.Phase = profile, "switching"
	return f.st, nil
}

func fleetOn(selected, serving, phase string) *fakeControl {
	return &fakeControl{st: enginecontrol.State{
		Selected: selected, Serving: serving, Phase: phase, Supervised: phase != "", ChosenBy: "deneb",
		ChosenAt: time.UnixMilli(1_789_800_000_000), UpdatedAt: time.UnixMilli(1_789_800_010_000),
		Profiles: []enginecontrol.Profile{{Name: "glm53", Model: "glm-5.3-flash"}, {Name: "qwen38", Model: "qwen3.8-flash-next"}},
	}}
}

func servingCall(t *testing.T, deps EngineDeps, method string, params any) (EngineServing, *protocol.ResponseFrame) {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		raw, _ = json.Marshal(params)
	}
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())
	resp := EngineMethods(deps)[method](ctx, &protocol.RequestFrame{ID: "1", Params: raw})
	var out EngineServing
	if resp.OK {
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
	}
	return out, resp
}

func withControl(c EngineControl) EngineDeps {
	return EngineDeps{Control: func() EngineControl { return c }}
}

func TestEngineServingSaysWhatProductionServesAndWhatWasChosen(t *testing.T) {
	out, resp := servingCall(t, withControl(fleetOn("qwen38", "glm53", "switching")), "miniapp.engine.serving", nil)
	if !resp.OK {
		t.Fatalf("error: %+v", resp.Error)
	}
	if !out.Available || out.Head != "choiceoh@srv2" || out.Reason != "" {
		t.Fatalf("availability = %+v", out)
	}
	if out.Selected != "qwen38" || out.SelectedModel != "qwen3.8-flash-next" || out.Serving != "glm53" ||
		out.ServingModel != "glm-5.3-flash" || out.Phase != "switching" || !out.Supervised {
		t.Errorf("state = %+v", out)
	}
	if len(out.Profiles) != 2 || out.Profiles[1].Model != "qwen3.8-flash-next" || out.ChosenAtMs != 1_789_800_000_000 {
		t.Errorf("profiles/chosen = %+v / %d", out.Profiles, out.ChosenAtMs)
	}
}

// "Not configured", "the fleet has no selection yet" and "the head did not
// answer" are three different things to do something about.
func TestEngineServingSaysWhyItCannotAsk(t *testing.T) {
	cases := map[string]struct {
		deps EngineDeps
		want string
	}{
		"no control":  {EngineDeps{}, "엔진 제어가 설정되어 있지 않습니다"},
		"nil control": {EngineDeps{Control: func() EngineControl { return nil }}, "엔진 제어가 설정되어 있지 않습니다"},
		"old fleet": {
			withControl(&fakeControl{stateErr: errors.New(
				"ssh choiceoh@srv2: exit status 2 (python3: can't open file '/home/choiceoh/st-engine/launchers/st_production.py')",
			)}),
			"플릿이 모델 선택을 아직 지원하지 않습니다 (st_production.py 없음)",
		},
		"unreachable": {
			withControl(&fakeControl{stateErr: errors.New("ssh choiceoh@srv2: signal: killed ()")}),
			"플릿에 닿지 못했습니다: ssh choiceoh@srv2: signal: killed ()",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out, resp := servingCall(t, tc.deps, "miniapp.engine.serving", nil)
			if !resp.OK {
				t.Fatalf("error: %+v", resp.Error)
			}
			if out.Available || out.Reason != tc.want {
				t.Errorf("available=%v reason=%q, want %q", out.Available, out.Reason, tc.want)
			}
			if out.Profiles == nil || out.Moved == nil || out.Held == nil {
				t.Error("empty lists must marshal as [] so the app never sees null")
			}
		})
	}
}

func TestEngineSelectWritesTheChoiceAndAnswersWithTheFleetsState(t *testing.T) {
	fleet := fleetOn("glm53", "glm53", "serving")
	out, resp := servingCall(t, withControl(fleet), "miniapp.engine.select", map[string]string{"profile": "qwen38"})
	if !resp.OK {
		t.Fatalf("error: %+v", resp.Error)
	}
	if len(fleet.selected) != 1 || fleet.selected[0] != "qwen38|deneb|Deneb 앱에서 선택" {
		t.Fatalf("select calls = %v", fleet.selected)
	}
	if out.Selected != "qwen38" || out.Phase != "switching" || out.Serving != "glm53" {
		t.Errorf("answer = %+v", out)
	}
}

func TestEngineSelectRefusesWhatItCannotDo(t *testing.T) {
	fleet := fleetOn("glm53", "glm53", "serving")
	if _, resp := servingCall(t, withControl(fleet), "miniapp.engine.select", map[string]string{"profile": ""}); resp.OK {
		t.Error("an empty profile was accepted")
	}
	_, resp := servingCall(t, withControl(fleet), "miniapp.engine.select", map[string]string{"profile": "qwen39"})
	if resp.OK || resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("unknown profile: %+v", resp.Error)
	}
	if _, resp := servingCall(t, EngineDeps{}, "miniapp.engine.select", map[string]string{"profile": "qwen38"}); resp.OK {
		t.Error("a gateway without the control accepted a choice")
	}
	if len(fleet.selected) != 0 {
		t.Errorf("a refused choice was written: %v", fleet.selected)
	}
}

// After a switch the screen says which roles went with the engine, and which
// stayed and why — read from the routing follow's record.
func TestEngineServingCarriesTheRoutingFollow(t *testing.T) {
	log := &enginecontrol.FollowLog{}
	log.Record(enginecontrol.FollowReport{
		At: time.UnixMilli(1_789_800_100_000), Served: "qwen3.8-flash-next",
		Moves: []enginecontrol.Move{{Role: "coding", From: "wormhole/glm-5.3-flash", To: "wormhole/qwen3.8-flash-next"}},
		Skips: []enginecontrol.Skip{{Role: "vision", From: "wormhole/glm-5.3-flash", Reason: "qwen3.8-flash-next 엔트리는 이미지를 받지 않습니다"}},
	})
	deps := withControl(fleetOn("qwen38", "qwen38", "serving"))
	deps.Follow = func() *enginecontrol.FollowLog { return log }
	out, _ := servingCall(t, deps, "miniapp.engine.serving", nil)
	if out.FollowAtMs != 1_789_800_100_000 || out.FollowServed != "qwen3.8-flash-next" {
		t.Errorf("follow = %d %q", out.FollowAtMs, out.FollowServed)
	}
	if len(out.Moved) != 1 || out.Moved[0].To != "wormhole/qwen3.8-flash-next" || len(out.Held) != 1 || out.Held[0].Role != "vision" {
		t.Errorf("moved %+v held %+v", out.Moved, out.Held)
	}
}

func TestEngineServingAndSelectRequireIdentity(t *testing.T) {
	for _, m := range []string{"miniapp.engine.serving", "miniapp.engine.select"} {
		resp := EngineMethods(EngineDeps{})[m](context.Background(), &protocol.RequestFrame{ID: "1"})
		if resp.OK || resp.Error == nil || resp.Error.Code != protocol.ErrUnauthorized {
			t.Errorf("%s without identity: %+v", m, resp.Error)
		}
	}
}
