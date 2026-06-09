// skills.go — miniapp.skills.* RPC handlers.
//
// Exposes the workspace skill catalog to the native client settings
// (DenebConfigScreen Skills tab) as a read-only list ("which skills does
// this agent have?"). The skills.* RPC surface (skill/ handler) already
// covers the full snapshot/install/configure flow for richer consumers;
// this slim projection is presentation-only.
//
// The skills are pre-filtered by the caller (chat.EligibleWorkspaceSkills)
// through the same archived + eligibility passes the system prompt applies,
// so the tab advertises only skills the agent can actually use — not the raw
// discovery result, which would include archived or env/bin-ineligible skills.

package handlerminiapp

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// SkillRow is one entry in the Settings skills list. A slim projection of
// skills.SkillEntry — only the fields the read-only list renders.
//
//deneb:wire
type SkillRow struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	// Source is the discovery origin: managed | workspace |
	// agents-skills-personal | agents-skills-project | bundled | plugin | extra.
	Source  string `json:"source,omitempty"`
	Version string `json:"version,omitempty"`
	// UserInvocable reports whether the skill can be triggered as a slash
	// command (frontmatter user-invocable, default true).
	UserInvocable bool `json:"userInvocable"`
}

// SkillsListResponse is the miniapp.skills.list payload.
//
//deneb:wire
type SkillsListResponse struct {
	Skills []SkillRow `json:"skills"`
	Count  int        `json:"count"`
}

// SkillsDeps provides the already-filtered workspace skills. List returns the
// skills after the archived + eligibility passes (see chat.EligibleWorkspaceSkills),
// keeping this handler presentation-only. A nil List disables the domain so
// method_registry can register conditionally.
type SkillsDeps struct {
	List func() []skills.SkillEntry
}

// SkillsMethods returns the miniapp.skills.* handler map, or nil when no
// skills provider is wired.
func SkillsMethods(deps SkillsDeps) map[string]rpcutil.HandlerFunc {
	if deps.List == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.skills.list": skillsList(deps),
	}
}

func skillsList(deps SkillsDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}

		entries := deps.List()

		// entries arrive sorted by name from discovery; the front-end can
		// re-group by category/source without losing a stable secondary order.
		rows := make([]SkillRow, 0, len(entries))
		for _, e := range entries {
			row := SkillRow{
				Name:        e.Skill.Name,
				Description: e.Skill.Description,
				Category:    e.Skill.Category,
				Source:      string(e.Skill.Source),
				Version:     e.Skill.Version,
			}
			if e.Invocation != nil {
				row.UserInvocable = e.Invocation.UserInvocable
			}
			rows = append(rows, row)
		}

		return rpcutil.RespondOK(req.ID, SkillsListResponse{Skills: rows, Count: len(rows)})
	}
}
