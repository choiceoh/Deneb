package modelpicker

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginecontrol"
)

// FollowEngine moves the roles on the local engine to the router entries of
// the model it serves (enginecontrol.PlanFollow), through the picker's own
// persist-and-apply path — the same one a tap in the native picker takes, so
// deneb.json and the live registry agree and no restart is needed. A move the
// picker refuses (the entry is not a model it offers) comes back as a skip
// with the reason, and the role stays where it was.
func (s *Controller) FollowEngine(ctx context.Context, entries []enginecontrol.Entry, served string) ([]enginecontrol.Move, []enginecontrol.Skip) {
	roles := make(map[string]string)
	for _, r := range s.roleMiniappModels() {
		if r.Model != "" {
			roles[r.Role] = r.Model
		}
	}
	planned, skips := enginecontrol.PlanFollow(roles, entries, served)
	moves := make([]enginecontrol.Move, 0, len(planned))
	for _, m := range planned {
		if _, err := s.setMiniappModel(ctx, m.Role, m.To); err != nil {
			skips = append(skips, enginecontrol.Skip{Role: m.Role, From: m.From, Reason: m.To + " 로 옮기지 못했습니다: " + err.Error()})
			continue
		}
		moves = append(moves, m)
	}
	return moves, skips
}
