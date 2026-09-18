package server

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
)

// mailAnalysisSynthesisEndpoints is RoleMain's fallback chain for tool-less
// mail stage-2. Agent synthesis (system:mailpoll) often skips that chain after
// wiki/mail_archive tools, then the previous single StreamChat retried the same
// dead main on an already-cancelled context — production analyses all failed.
func (s *Server) mailAnalysisSynthesisEndpoints() []mailanalysis.SynthesisEndpoint {
	if s.modelRegistry == nil {
		return nil
	}
	chain := s.modelRegistry.FallbackChain(modelrole.RoleMain)
	out := make([]mailanalysis.SynthesisEndpoint, 0, len(chain))
	seen := map[string]bool{}
	for i, role := range chain {
		cfg := s.modelRegistry.RefreshVllmRole(role)
		client := s.modelRegistry.Client(role)
		if client == nil || cfg.Model == "" || seen[cfg.Model] {
			continue
		}
		if s.modelRegistry.ModelUnhealthy(cfg.Model) && s.mailAnalysisHasHealthyEndpoint(chain[i+1:], seen, cfg.Model) {
			s.logger.Warn("mail analysis: skipping unhealthy stage-2 model (advisory)", "role", string(role), "model", cfg.Model)
			continue
		}
		seen[cfg.Model] = true
		kwarg := s.modelRegistry.CapabilityForModel(cfg.ProviderID, cfg.Model).ThinkingToggleKwarg
		out = append(out, mailanalysis.SynthesisEndpoint{
			Client:        client,
			Model:         cfg.Model,
			ThinkingKwarg: kwarg,
		})
	}
	return out
}

func (s *Server) mailAnalysisHasHealthyEndpoint(rest []modelrole.Role, seen map[string]bool, skipModel string) bool {
	if s.modelRegistry == nil {
		return false
	}
	for _, role := range rest {
		cfg := s.modelRegistry.Config(role)
		if cfg.Model == "" || cfg.Model == skipModel || seen[cfg.Model] {
			continue
		}
		if s.modelRegistry.Client(role) == nil {
			continue
		}
		if !s.modelRegistry.ModelUnhealthy(cfg.Model) {
			return true
		}
	}
	return false
}

func (s *Server) mailAnalysisRecordFailure(model string) {
	if s == nil || s.modelRegistry == nil || strings.TrimSpace(model) == "" {
		return
	}
	s.modelRegistry.RecordModelFailure(model)
}

func (s *Server) mailAnalysisShouldSkipAgent() bool {
	if s == nil || s.modelRegistry == nil {
		return false
	}
	cfg := s.modelRegistry.Config(modelrole.RoleMain)
	if cfg.Model == "" || !s.modelRegistry.ModelUnhealthy(cfg.Model) {
		return false
	}
	chain := s.modelRegistry.FallbackChain(modelrole.RoleMain)
	if len(chain) < 2 {
		return false
	}
	return s.mailAnalysisHasHealthyEndpoint(chain[1:], map[string]bool{}, cfg.Model)
}

func incompleteMailAgentSynthesis(result *chat.SyncResult) bool {
	if result == nil {
		return true
	}
	switch result.StopReason {
	case "timeout", "aborted", "error", "max_turns", "max_turns_graceful":
		return true
	}
	return strings.TrimSpace(result.BestText()) == ""
}

func incompleteMailAgentError(result *chat.SyncResult) error {
	if result == nil {
		return fmt.Errorf("agent synthesis returned nil result")
	}
	if result.StopReason != "" {
		return fmt.Errorf("agent synthesis incomplete: %s", result.StopReason)
	}
	return fmt.Errorf("agent synthesis empty")
}
