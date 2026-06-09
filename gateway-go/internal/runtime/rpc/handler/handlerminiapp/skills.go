// skills.go — miniapp.skills.* RPC handlers.
//
// Exposes the workspace skill catalog to the native client's Settings
// screen as a read-only list ("which skills does this agent have?"). The
// skills.* RPC surface (skill/ handler) already covers the full
// snapshot/install/configure flow for richer consumers, but it requires
// the caller to pass workspaceDir + bundled/managed dirs explicitly. The
// native client doesn't know server-side paths, so this handler resolves
// the workspace itself and returns a slim, presentation-ready row set.
//
// Discovery here intentionally uses WorkspaceDir only — the same config
// the system-prompt builder uses (run_exec_skills.go:loadCachedSkillsPrompt).
// That means this list matches the skills the agent actually sees:
// managed (~/.deneb/skills), personal (~/.agents/skills), project, and
// workspace skills. Bundled/plugin dirs are not injected (the prompt path
// doesn't inject them either), so the two surfaces stay consistent.

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

// SkillsDeps holds the workspace resolver. WorkspaceDir is the only
// dependency — discovery fills the managed/personal/project paths from
// $HOME internally. A nil resolver disables the domain (Methods returns
// nil so method_registry can register conditionally).
type SkillsDeps struct {
	WorkspaceDir func() string
}

// SkillsMethods returns the miniapp.skills.* handler map, or nil when no
// workspace resolver is wired.
func SkillsMethods(deps SkillsDeps) map[string]rpcutil.HandlerFunc {
	if deps.WorkspaceDir == nil {
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

		entries := skills.DiscoverWorkspaceSkills(skills.DiscoverConfig{
			WorkspaceDir: deps.WorkspaceDir(),
		})

		// entries arrive sorted by name from DiscoverWorkspaceSkills; the
		// front-end can re-group by category/source without losing a stable
		// secondary order.
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
