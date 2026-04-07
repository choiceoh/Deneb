// Package server — admin model hot-swap endpoint for dev/live testing.
//
// GET  /admin/model  → current model + available models (JSON)
// PUT  /admin/model  → set default model in-memory (no config persist)
package server

import (
	"encoding/json"
	"net/http"

	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
)

// handleAdminModelGet responds with the current default model and available models.
func (s *Server) handleAdminModelGet(w http.ResponseWriter, _ *http.Request) {
	current := ""
	if s.chatHandler != nil {
		current = s.chatHandler.DefaultModel()
	}
	if current == "" && s.modelRegistry != nil {
		current = s.modelRegistry.FullModelID(modelrole.RoleMain)
	}

	type modelInfo struct {
		Role     string `json:"role"`
		FullID   string `json:"full_id"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}

	var available []modelInfo
	if s.modelRegistry != nil {
		for _, role := range []modelrole.Role{modelrole.RoleMain, modelrole.RoleLightweight, modelrole.RoleFallback} {
			cfg := s.modelRegistry.Config(role)
			if cfg.Model == "" {
				continue
			}
			available = append(available, modelInfo{
				Role:     string(role),
				FullID:   s.modelRegistry.FullModelID(role),
				Provider: cfg.ProviderID,
				Model:    cfg.Model,
			})
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"current":   current,
		"available": available,
	})
}

// handleAdminModelPut sets the default model in-memory (hot-swap).
// Body: {"model": "zai/glm-5-turbo"} or {"model": "google/gemini-3.1-pro-preview"}
func (s *Server) handleAdminModelPut(w http.ResponseWriter, r *http.Request) {
	if s.chatHandler == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "chat handler not initialized",
		})
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON: " + err.Error(),
		})
		return
	}
	if req.Model == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "model field required",
		})
		return
	}

	// Resolve role names (e.g., "main", "lightweight") to full model IDs.
	resolved := req.Model
	if s.modelRegistry != nil {
		if fullID, _, ok := s.modelRegistry.ResolveModel(req.Model); ok {
			resolved = fullID
		}
	}

	prev := s.chatHandler.DefaultModel()
	s.chatHandler.SetDefaultModel(resolved)

	s.logger.Info("admin: model hot-swapped", "prev", prev, "new", resolved)

	s.writeJSON(w, http.StatusOK, map[string]any{
		"previous": prev,
		"current":  resolved,
	})
}
