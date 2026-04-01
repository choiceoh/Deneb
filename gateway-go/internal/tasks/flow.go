package tasks

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
)

// CreateFlowParams holds parameters for creating a new flow.
type CreateFlowParams struct {
	Label            string
	OwnerKey         string
	ParentSessionKey string
}

// CreateFlow creates a new active flow.
func CreateFlow(reg *Registry, p CreateFlowParams) (*FlowRecord, error) {
	now := NowMs()
	f := &FlowRecord{
		FlowID:           shortid.New("flow"),
		Label:            p.Label,
		Status:           FlowActive,
		OwnerKey:         p.OwnerKey,
		ParentSessionKey: p.ParentSessionKey,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := reg.PutFlow(f); err != nil {
		return nil, fmt.Errorf("create flow: %w", err)
	}
	return f, nil
}

// LinkTaskToFlow associates a task with a flow and refreshes flow counts.
func LinkTaskToFlow(reg *Registry, taskID, flowID string) error {
	t := reg.Get(taskID)
	if t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	f := reg.GetFlow(flowID)
	if f == nil {
		return fmt.Errorf("flow not found: %s", flowID)
	}

	t.FlowID = flowID
	if err := reg.Put(t); err != nil {
		return err
	}

	return reg.RefreshFlowCounts(flowID)
}

// ResumeBlockedFlow unblocks a flow by resuming all blocked tasks in it.
func ResumeBlockedFlow(reg *Registry, flowID string) (int, error) {
	f := reg.GetFlow(flowID)
	if f == nil {
		return 0, fmt.Errorf("flow not found: %s", flowID)
	}
	if f.Status != FlowBlocked {
		return 0, fmt.Errorf("flow is not blocked: %s", f.Status)
	}

	tasks := reg.ListByFlowID(flowID)
	resumed := 0
	for _, t := range tasks {
		if t.Status == StatusBlocked {
			if err := StartTask(reg, t.TaskID); err == nil {
				resumed++
			}
		}
	}

	if resumed > 0 {
		f.Status = FlowActive
		f.UpdatedAt = NowMs()
		_ = reg.PutFlow(f)
	}

	return resumed, nil
}
