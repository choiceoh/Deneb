package feedback

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
)

const UserSimulatorPlanSchemaVersion = "deneb-briefcase-user-simulator/v1"

var ErrInvalidUserSimulatorPlan = errors.New("invalid briefcase user simulator plan")

// UserSimulator receives only the construction-locked public handoff. It has
// no supervisor plan, checkpoint report, hidden reference, or casepack handle.
type UserSimulator interface {
	NextFeedback(context.Context, int, SimulatorHandoff) (string, error)
}

// UserSimulatorPlan is a sealed deterministic simulator fixture. A future
// model-backed simulator can implement UserSimulator without changing the
// information-firewall or loop contracts.
type UserSimulatorPlan struct {
	SchemaVersion string             `json:"schemaVersion"`
	CaseID        string             `json:"caseId"`
	FollowUps     []ScriptedFollowUp `json:"followUps"`
}

type ScriptedFollowUp struct {
	Cycle       int             `json:"cycle"`
	WhenVerdict VerdictCategory `json:"whenVerdict"`
	Message     string          `json:"message"`
}

type ScriptedUserSimulator struct {
	mu          sync.Mutex
	followUps   []ScriptedFollowUp
	nextCycle   int
	maxFollowUp int
}

// NewScriptedUserSimulator validates a sealed plan against the public signed
// follow-up budget. The plan must cover every authorized cycle exactly once so
// a CONTINUE decision can never fall through to an implicit or ambient input.
func NewScriptedUserSimulator(plan UserSimulatorPlan, authorizedMax int) (*ScriptedUserSimulator, error) {
	if plan.SchemaVersion != UserSimulatorPlanSchemaVersion {
		return nil, userSimulatorPlanError("unsupported schemaVersion")
	}
	if strings.TrimSpace(plan.CaseID) == "" {
		return nil, userSimulatorPlanError("caseId is required")
	}
	if authorizedMax < 0 || authorizedMax > casepack.MaxFollowUpsV1 {
		return nil, userSimulatorPlanError("authorized follow-up budget is outside the v1 range")
	}
	if len(plan.FollowUps) != authorizedMax {
		return nil, userSimulatorPlanError("followUps must exactly cover the authorized budget")
	}
	followUps := append([]ScriptedFollowUp(nil), plan.FollowUps...)
	for index, followUp := range followUps {
		wantCycle := index + 1
		if followUp.Cycle != wantCycle {
			return nil, userSimulatorPlanError("followUps must use consecutive one-based cycles")
		}
		if followUp.WhenVerdict != VerdictNeedsRevision {
			return nil, userSimulatorPlanError("scripted follow-ups may target only needs-revision")
		}
		if !utf8.ValidString(followUp.Message) || strings.TrimSpace(followUp.Message) == "" {
			return nil, userSimulatorPlanError("follow-up message must be non-empty valid UTF-8")
		}
		if utf8.RuneCountInString(followUp.Message) > defaultMaxFollowUpRunes {
			return nil, userSimulatorPlanError("follow-up message exceeds the v1 runtime limit")
		}
	}
	return &ScriptedUserSimulator{followUps: followUps, nextCycle: 1, maxFollowUp: authorizedMax}, nil
}

func (s *ScriptedUserSimulator) NextFeedback(ctx context.Context, cycle int, handoff SimulatorHandoff) (string, error) {
	if s == nil {
		return "", userSimulatorPlanError("simulator is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if handoff.VerdictCategory() != VerdictNeedsRevision || !handoff.Recoverable() {
		return "", userSimulatorPlanError("handoff is not recoverable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cycle != s.nextCycle {
		return "", userSimulatorPlanError("cycle is duplicated or out of order")
	}
	if cycle <= 0 || cycle > s.maxFollowUp || cycle > len(s.followUps) {
		return "", userSimulatorPlanError("follow-up budget is exhausted")
	}
	followUp := s.followUps[cycle-1]
	if followUp.WhenVerdict != handoff.VerdictCategory() {
		return "", userSimulatorPlanError("no script matches the public verdict")
	}
	s.nextCycle++
	return followUp.Message, nil
}

func userSimulatorPlanError(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidUserSimulatorPlan, reason)
}
