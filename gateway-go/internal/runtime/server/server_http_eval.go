package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
)

// handleEvalExtract runs a PRODUCTION extractor (its real prompt + jsonutil parse +
// post-processing) against a named model through the wormhole router, so a benchmark
// can score the real extraction path instead of a raw model probe. The model is a
// wormhole route name (glm-5.2 / qwen3.6-35b-a3b / deepseek-v4-flash / mimo-v2.5-pro)
// — no arbitrary URL, so no SSRF surface. Operator tool, client-token guarded.
//
//	POST /api/eval/extract  {kind: deal|facts|actions, input, model}
//	  → {ok: true, result: <DealInfo | facts-markdown | []ActionItem>}
//
// Because the extractor parses with jsonutil (strips ```json fences + thinking
// tags) the result is fence-immune — the difference prod never sees and the raw
// sparkfleet probe wrongly penalized.
func (s *Server) handleEvalExtract(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticateMiniappRequest(w, r); !ok {
		return
	}
	var req struct {
		Kind  string `json:"kind"`
		Input string `json:"input"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request: " + err.Error()})
		return
	}
	req.Kind, req.Model = strings.TrimSpace(req.Kind), strings.TrimSpace(req.Model)
	if req.Kind == "" || req.Model == "" || strings.TrimSpace(req.Input) == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "kind, input, and model are required"})
		return
	}
	if s.modelRegistry == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "model registry unavailable"})
		return
	}
	// wormhole fronts every benchmarkable model by name; the lightweight role's
	// client is wormhole-backed too, so it is a safe fallback.
	client := s.modelRegistry.ClientForProvider("wormhole")
	if client == nil {
		client = s.modelRegistry.Client(modelrole.RoleLightweight)
	}
	if client == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "no wormhole-backed client configured"})
		return
	}

	result, err := gmailpoll.ExtractForEval(r.Context(), client, req.Model, req.Kind, req.Input)
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "model": req.Model, "kind": req.Kind, "result": result})
}
