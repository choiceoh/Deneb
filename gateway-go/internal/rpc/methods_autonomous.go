package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/autonomous"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// AutonomousDeps holds the dependencies for autonomous RPC methods.
type AutonomousDeps struct {
	Autonomous *autonomous.Service
}

// RegisterAutonomousMethods registers the autonomous mode RPC methods.
// These match the CLI expectations in cli-rs/src/subcli/autonomous.rs.
func RegisterAutonomousMethods(d *Dispatcher, deps AutonomousDeps) {
	if deps.Autonomous == nil {
		return
	}
	d.Register("autonomous.status", autonomousStatus(deps))
	d.Register("autonomous.goals.list", autonomousGoalsList(deps))
	d.Register("autonomous.goals.add", autonomousGoalsAdd(deps))
	d.Register("autonomous.goals.remove", autonomousGoalsRemove(deps))
	d.Register("autonomous.cycle.run", autonomousCycleRun(deps))
	d.Register("autonomous.cycle.stop", autonomousCycleStop(deps))
}

func autonomousStatus(deps AutonomousDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		status := deps.Autonomous.Status()
		return protocol.MustResponseOK(req.ID, status)
	}
}

func autonomousGoalsList(deps AutonomousDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		goals, err := deps.Autonomous.Goals().List()
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "failed to load goals: "+err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"goals": goals,
			"count": len(goals),
		})
	}
}

func autonomousGoalsAdd(deps AutonomousDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Description string `json:"description"`
			Priority    string `json:"priority"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.Description == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "description is required"))
		}

		goal, err := deps.Autonomous.AddGoal(p.Description, p.Priority)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "failed to add goal: "+err.Error()))
		}
		return protocol.MustResponseOK(req.ID, goal)
	}
}

func autonomousGoalsRemove(deps AutonomousDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}

		if err := deps.Autonomous.Goals().Remove(p.ID); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{"removed": p.ID})
	}
}

func autonomousCycleRun(deps AutonomousDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		outcome, err := deps.Autonomous.RunCycle(ctx)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, outcome)
	}
}

func autonomousCycleStop(deps AutonomousDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		deps.Autonomous.StopCycle()
		return protocol.MustResponseOK(req.ID, map[string]any{"stopped": true})
	}
}
