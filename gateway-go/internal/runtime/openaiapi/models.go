package openaiapi

import (
	"net/http"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// modelAliases is the set of stable identifiers exposed via /v1/models.
// The IDE client (Zed/OpenCode) sends one of these as the request
// "model" field; chat.completions resolves it to the underlying
// provider/model at request time, so role swaps in deneb.json take
// effect without IDE reconfiguration.
var modelAliases = []struct {
	ID   string
	Role modelrole.Role
}{
	{ID: "deneb-main", Role: modelrole.RoleMain},
	{ID: "deneb-light", Role: modelrole.RoleLightweight},
	{ID: "deneb-fallback", Role: modelrole.RoleFallback},
}

func (r *routes) handleModels(w http.ResponseWriter, _ *http.Request) {
	var created int64
	if r.deps.StartedAt != nil {
		if t := r.deps.StartedAt(); !t.IsZero() {
			created = t.Unix()
		}
	}
	out := ModelsList{Object: "list", Data: []Model{}}
	for _, a := range modelAliases {
		if r.deps.ModelRegistry == nil {
			continue
		}
		if r.deps.ModelRegistry.FullModelID(a.Role) == "" {
			continue
		}
		out.Data = append(out.Data, Model{
			ID:      a.ID,
			Object:  "model",
			Created: created,
			OwnedBy: "deneb",
		})
	}
	writeJSON(w, http.StatusOK, out)
}
