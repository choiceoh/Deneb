package enginecontrol

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginelive"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

type recordingPicker struct {
	served []string
	vision []*bool
	moves  []Move
	skips  []Skip
}

func (p *recordingPicker) FollowEngine(_ context.Context, _ []Entry, served string, servedVision *bool) ([]Move, []Skip) {
	p.served = append(p.served, served)
	p.vision = append(p.vision, servedVision)
	return p.moves, p.skips
}

func sees(v *bool) func(context.Context, string, string) *bool {
	return func(context.Context, string, string) *bool { return v }
}

type fixedLiveness struct{ down bool }

func (l fixedLiveness) State(string) (enginelive.EngineState, bool) {
	return enginelive.EngineState{Down: l.down}, true
}

func door(owner, model string) func(context.Context, string) (observe.EngineFleet, bool) {
	return func(context.Context, string) (observe.EngineFleet, bool) {
		return observe.EngineFleet{Owner: owner, Model: model, FleetKnown: owner != ""}, true
	}
}

func TestProductionServingAnotherModelMovesTheRoles(t *testing.T) {
	picker := &recordingPicker{moves: []Move{{Role: "coding", From: "wormhole/glm-5.3-flash", To: "wormhole/qwen3.8-flash-next"}}}
	log := &FollowLog{}
	task := &FollowTask{
		Endpoint: "http://10.0.0.5:8000/metrics", Picker: picker, Log: log,
		Fleet: door("production/srv2/1368796", "qwen3.8-flash-next"), Vision: sees(nil),
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(picker.served, ",") != "qwen3.8-flash-next" {
		t.Fatalf("picker asked for %v", picker.served)
	}
	if got := log.Last(); got.Served != "qwen3.8-flash-next" || len(got.Moves) != 1 {
		t.Errorf("log = %+v", got)
	}
}

// A campaign window boots another model for an hour; production is still the
// other one. Following the window would move every local role away from the
// model production comes back with.
func TestAWindowMovesNothing(t *testing.T) {
	for _, owner := range []string{"session/qwen38-s2h6-0919", "queue/qwen38-qsa-geometry-0919", "choiceoh@srv2/1204007", ""} {
		picker := &recordingPicker{}
		task := &FollowTask{Endpoint: "e", Picker: picker, Fleet: door(owner, "qwen3.8-flash-next"), Vision: sees(nil)}
		if err := task.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(picker.served) != 0 {
			t.Errorf("owner %q moved roles", owner)
		}
	}
}

func TestADownEngineIsNotAsked(t *testing.T) {
	picker := &recordingPicker{}
	asked := false
	task := &FollowTask{
		Endpoint: "e", Picker: picker, Liveness: fixedLiveness{down: true},
		Fleet: func(context.Context, string) (observe.EngineFleet, bool) {
			asked = true
			return observe.EngineFleet{}, false
		},
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if asked || len(picker.served) != 0 {
		t.Fatalf("a down engine was asked (%v) or followed (%v)", asked, picker.served)
	}
}

// Vision is held back on every pass while the engine serves a text-only
// model: said once, not every 30 s — and said again if it comes back after it cleared.
func TestAHeldBackRoleIsWarnedOnce(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	skip := Skip{Role: "vision", From: "wormhole/glm-5.3-flash", Reason: "qwen3.8-flash-next 엔트리는 이미지를 받지 않습니다"}
	picker := &recordingPicker{skips: []Skip{skip}}
	task := &FollowTask{Endpoint: "e", Picker: picker, Logger: logger, Fleet: door("production/srv2/1", "qwen3.8-flash-next"), Vision: sees(nil)}
	for range 3 {
		_ = task.Run(context.Background())
	}
	if n := strings.Count(buf.String(), "cannot follow"); n != 1 {
		t.Fatalf("warned %d times over three passes, want once:\n%s", n, buf.String())
	}
	picker.skips = nil
	_ = task.Run(context.Background())
	picker.skips = []Skip{skip}
	_ = task.Run(context.Background())
	if n := strings.Count(buf.String(), "cannot follow"); n != 2 {
		t.Fatalf("warned %d times, want again after it cleared", n)
	}
}

// The pass asks the door whether this boot of the served model takes images
// and hands the answer to the picker with the model — the planner decides.
func TestThePassCarriesTheEnginesVisionReport(t *testing.T) {
	picker := &recordingPicker{}
	asked := ""
	task := &FollowTask{
		Endpoint: "http://10.0.0.5:8000/metrics", Picker: picker,
		Fleet: door("production/srv2/1", "qwen3.8-flash-next"),
		Vision: func(_ context.Context, metricsURL, model string) *bool {
			asked = metricsURL + " " + model
			v := true
			return &v
		},
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if asked != "http://10.0.0.5:8000/metrics qwen3.8-flash-next" {
		t.Errorf("asked %q", asked)
	}
	if len(picker.vision) != 1 || picker.vision[0] == nil || !*picker.vision[0] {
		t.Errorf("picker got vision %v", picker.vision)
	}
}
