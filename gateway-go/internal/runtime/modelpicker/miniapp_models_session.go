package modelpicker

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// sessionBinder is the session.Manager surface the chat picker needs to bind a
// model to one conversation without touching the global role default.
type sessionBinder interface {
	Patch(key string, patch session.PatchFields) *session.Session
}

func (s *Controller) resolveAllowedMiniappModel(ctx context.Context, requested string) (string, error) {
	modelID := strings.TrimSpace(requested)
	if s.modelRegistry != nil {
		if resolved, _, ok := s.modelRegistry.ResolveModel(modelID); ok {
			modelID = resolved
		}
	}

	allowed := false
	for _, section := range s.miniappModelSections(ctx) {
		for _, entry := range section.entries {
			if entry.fullID == modelID {
				allowed = true
				break
			}
		}
		if allowed {
			break
		}
	}
	if !allowed {
		return "", rpcerr.Newf(protocol.ErrNotFound, "model not available: %s", requested)
	}
	return modelID, nil
}

func (s *Controller) setMiniappSessionModel(ctx context.Context, sessionKey, requested string) (string, error) {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return "", rpcerr.MissingParam("sessionKey")
	}
	if s.sessions == nil {
		return "", rpcerr.Unavailable("session manager is not ready")
	}
	// Empty id clears the per-conversation override so the next turn
	// follows the global role default again.
	if strings.TrimSpace(requested) == "" {
		empty := ""
		s.sessions.Patch(key, session.PatchFields{Model: &empty})
		return "", nil
	}
	modelID, err := s.resolveAllowedMiniappModel(ctx, requested)
	if err != nil {
		return "", err
	}
	s.sessions.Patch(key, session.PatchFields{Model: &modelID})
	return modelID, nil
}
