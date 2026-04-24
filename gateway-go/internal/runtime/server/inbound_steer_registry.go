// inbound_steer_registry.go — Adapter that binds *acp.ACPRegistry to the
// SubagentLookup interface used by parseSteerCommand.
package server

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/acp"
	subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/subagent"
)

// acpSubagentLookup wraps *acp.ACPRegistry.
type acpSubagentLookup struct {
	registry *acp.ACPRegistry
}

// newACPSubagentLookup returns an adapter, or nil if registry is nil.
func newACPSubagentLookup(registry *acp.ACPRegistry) SubagentLookup {
	if registry == nil {
		return nil
	}
	return &acpSubagentLookup{registry: registry}
}

// HasSubagent returns true when token resolves to an active subagent owned
// by sessionKey. Terminal runs (done/failed/killed, EndedAt != 0) are
// excluded so /steer can't dispatch into a dead session.
func (a *acpSubagentLookup) HasSubagent(sessionKey, token string) bool {
	if a == nil || a.registry == nil {
		return false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}

	agents := a.registry.List(sessionKey)
	if len(agents) == 0 {
		return false
	}
	records := make([]subagentpkg.SubagentRunRecord, 0, len(agents))
	for _, agent := range agents {
		if agent.EndedAt != 0 {
			continue
		}
		records = append(records, subagentpkg.SubagentRunRecord{
			RunID:           agent.ID,
			ChildSessionKey: agent.SessionKey,
			ControllerKey:   agent.ParentID,
			RequesterKey:    agent.ParentID,
			Label:           agent.Role,
			CreatedAt:       agent.SpawnedAt,
			StartedAt:       agent.SpawnedAt,
			EndedAt:         agent.EndedAt,
			OutcomeStatus:   agent.Status,
		})
	}
	if len(records) == 0 {
		return false
	}

	run, errMsg := subagentpkg.ResolveSubagentTarget(records, token)
	return run != nil && errMsg == ""
}
