package handlerminiapp

import (
	"context"
	"errors"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginecontrol"
	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// EngineServingProfile is one model the engine's production can serve.
//
//deneb:wire
type EngineServingProfile struct {
	Profile string `json:"profile"`
	Model   string `json:"model"`
}

// EngineRoleMove is a role that went with the engine to the model it serves.
//
//deneb:wire
type EngineRoleMove struct {
	Role string `json:"role"`
	From string `json:"from"`
	To   string `json:"to"`
}

// EngineRoleHold is a role on the engine that could not follow it, and why.
//
//deneb:wire
type EngineRoleHold struct {
	Role   string `json:"role"`
	From   string `json:"from"`
	Reason string `json:"reason"`
}

// EngineServing is the miniapp.engine.serving / miniapp.engine.select
// response: which model the engine's production serves, which one it is to
// serve, and what the switch is doing. The fleet decides — stkernel's
// st_production.py on the engine's head holds the selection and its
// supervisor does the switch — and the gateway asks it over ssh.
//
//deneb:wire
type EngineServing struct {
	NowMs int64 `json:"nowMs"`
	// Available is false when the gateway cannot ask the fleet: no engine, the
	// control switched off, the head unreachable, or a fleet whose tree has no
	// st_production.py yet. Reason says which, in the operator's words.
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	Head      string `json:"head,omitempty"`

	Selected      string `json:"selected,omitempty"`
	SelectedModel string `json:"selectedModel,omitempty"`
	ChosenBy      string `json:"chosenBy,omitempty"`
	ChosenAtMs    int64  `json:"chosenAtMs,omitempty"`
	Note          string `json:"note,omitempty"`

	// Supervised is false while no supervisor that knows the selection has
	// said anything: a choice can be written, but nobody would take it.
	Supervised   bool   `json:"supervised"`
	Serving      string `json:"serving,omitempty"`
	ServingModel string `json:"servingModel,omitempty"`
	// Phase is the supervisor's: serving, switching, launching, waiting,
	// held or reverted. Detail is its own sentence for it.
	Phase       string `json:"phase,omitempty"`
	Detail      string `json:"detail,omitempty"`
	UpdatedAtMs int64  `json:"updatedAtMs,omitempty"`

	Profiles []EngineServingProfile `json:"profiles"`

	// The routing follow: when the local roles last went with the engine, to
	// which model, and which roles it holds back now.
	FollowAtMs   int64            `json:"followAtMs,omitempty"`
	FollowServed string           `json:"followServed,omitempty"`
	Moved        []EngineRoleMove `json:"moved"`
	Held         []EngineRoleHold `json:"held"`
}

// EngineControl asks the fleet which model production serves and chooses it
// (*enginecontrol.Controller).
type EngineControl interface {
	State(ctx context.Context) (enginecontrol.State, error)
	Select(ctx context.Context, profile, by, note string) (enginecontrol.State, error)
	Target() string
}

// engineSelectNote is what the selection file says about a choice made here.
const engineSelectNote = "Deneb 앱에서 선택"

func engineServing(deps EngineDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if minibind.Identity(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.engine.serving requires client identity context").Response(req.ID)
		}
		out := deps.servingBase()
		if control := deps.control(); control != nil {
			st, err := control.State(ctx)
			fillServing(&out, control.Target(), st, err)
		}
		return rpcutil.RespondOK(req.ID, out)
	}
}

func engineSelect(deps EngineDeps) rpcutil.HandlerFunc {
	type params struct {
		Profile string `json:"profile"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if minibind.Identity(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.engine.select requires client identity context").Response(req.ID)
		}
		return rpcutil.BindCtx[params](ctx, req, func(ctx context.Context, p params) (any, error) {
			profile := strings.TrimSpace(p.Profile)
			if profile == "" {
				return nil, rpcerr.MissingParam("profile")
			}
			control := deps.control()
			if control == nil {
				return nil, rpcerr.Unavailable("엔진 제어가 설정되어 있지 않습니다")
			}
			st, err := control.Select(ctx, profile, "deneb", engineSelectNote)
			if errors.Is(err, enginecontrol.ErrUnknownProfile) {
				return nil, rpcerr.InvalidRequest("플릿이 서빙하지 않는 모델입니다: " + profile)
			}
			if err != nil {
				return nil, rpcerr.WrapDependencyFailed("engine select", err)
			}
			out := deps.servingBase()
			fillServing(&out, control.Target(), st, nil)
			return out, nil
		})
	}
}

func (d EngineDeps) control() EngineControl {
	if d.Control == nil {
		return nil
	}
	return d.Control()
}

// servingBase is the answer before the fleet is asked: the clock, the follow's
// record, and "not available" until fillServing says otherwise.
func (d EngineDeps) servingBase() EngineServing {
	out := EngineServing{
		NowMs: d.now().UnixMilli(), Profiles: []EngineServingProfile{},
		Moved: []EngineRoleMove{}, Held: []EngineRoleHold{}, Reason: "엔진 제어가 설정되어 있지 않습니다",
	}
	var log *enginecontrol.FollowLog
	if d.Follow != nil {
		log = d.Follow()
	}
	last := log.Last()
	if !last.At.IsZero() {
		out.FollowAtMs, out.FollowServed = last.At.UnixMilli(), last.Served
	}
	for _, m := range last.Moves {
		out.Moved = append(out.Moved, EngineRoleMove{Role: m.Role, From: m.From, To: m.To})
	}
	for _, s := range last.Skips {
		out.Held = append(out.Held, EngineRoleHold{Role: s.Role, From: s.From, Reason: s.Reason})
	}
	return out
}

func fillServing(out *EngineServing, head string, st enginecontrol.State, err error) {
	out.Head = head
	if err != nil {
		out.Available, out.Reason = false, servingUnavailableReason(err)
		return
	}
	out.Available, out.Reason = true, ""
	out.Selected, out.SelectedModel = st.Selected, st.ModelOf(st.Selected)
	out.ChosenBy, out.Note = st.ChosenBy, st.Note
	if !st.ChosenAt.IsZero() {
		out.ChosenAtMs = st.ChosenAt.UnixMilli()
	}
	out.Supervised = st.Supervised
	out.Serving, out.ServingModel = st.Serving, st.ModelOf(st.Serving)
	out.Phase, out.Detail = st.Phase, st.Detail
	if !st.UpdatedAt.IsZero() {
		out.UpdatedAtMs = st.UpdatedAt.UnixMilli()
	}
	for _, p := range st.Profiles {
		out.Profiles = append(out.Profiles, EngineServingProfile{Profile: p.Name, Model: p.Model})
	}
}

// servingUnavailableReason turns a failed ask into the screen's sentence. A
// head reached but without the script is a fleet older than the selection —
// a different thing from a head that did not answer.
func servingUnavailableReason(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "can't open file") || strings.Contains(msg, "No such file"):
		return "플릿이 모델 선택을 아직 지원하지 않습니다 (st_production.py 없음)"
	case strings.Contains(msg, "Permission denied") || strings.Contains(msg, "Host key verification failed"):
		return "플릿 헤드에 ssh 로 들어가지 못했습니다"
	default:
		if r := []rune(msg); len(r) > 160 {
			msg = string(r[:160]) + "…"
		}
		return "플릿에 닿지 못했습니다: " + msg
	}
}
