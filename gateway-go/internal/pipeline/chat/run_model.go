package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

// modelResolution holds the resolved model, provider, and initial role
// for an agent run.
type modelResolution struct {
	model       string
	providerID  string
	initialRole modelrole.Role
}

// resolveModel determines the actual model ID and provider from the run
// parameters, session state, and model role registry. Pure function — no IO.
//
// Resolution order:
//  1. Explicit model from params (role name or raw model ID)
//  2. Session override (chat picker) for any session
//  3. Sub-agent default when spawned and still unset
//  4. defaultModel from config
//  5. Registry main role fallback
//  6. Second-pass role resolution for fallback values
//  7. Provider prefix extraction ("google/gemini" → provider="google")
//  8. Sub-agent provider remapping ("<provider>-subagent")
func resolveModel(
	params RunParams,
	deps runDeps,
	sess *session.Session,
) modelResolution {
	if deps.briefcaseMode {
		// A benchmark model ID is an opaque endpoint-owned identifier. In
		// particular, Hugging Face/vLLM IDs commonly contain '/', which the
		// production provider-prefix parser must not strip in this mode.
		return modelResolution{model: params.Model, initialRole: modelrole.RoleMain}
	}
	model := params.Model
	initialRole := modelrole.RoleMain

	if deps.registry != nil && model != "" {
		// Role name → resolve to actual model ID with fallback chain.
		if resolved, role, ok := deps.registry.ResolveModel(model); ok {
			model = resolved
			initialRole = role
		}
		// Raw model ID → no role mapping, no fallback chain (direct override).
	}
	if model == "" && sess != nil && sess.Model != "" {
		// Per-session override from the chat picker (or a spawn-time
		// model). Must win over the global default so switching the
		// model in one conversation cannot retarget every other session.
		model = sess.Model
	}
	if model == "" && sess != nil && sess.SpawnedBy != "" && deps.subagentDefaultModel != "" {
		model = deps.subagentDefaultModel
	}
	// Image turns route to the configured vision model (agents.visionModel →
	// RoleVision) so a main model with no vision tower (e.g. DeepSeek-V4-Flash)
	// never receives image blocks it would strip or reject. OPT-IN: FullModelID
	// is "" until configured, so this is a no-op and image turns fall through to
	// the main model exactly as before.
	if model == "" && deps.registry != nil && hasImageAttachment(params.Attachments) {
		if v := deps.registry.FullModelID(modelrole.RoleVision); v != "" {
			model = v
			initialRole = modelrole.RoleVision
		}
	}
	if model == "" {
		model = deps.callbacks.defaultModel
	}
	if model == "" && deps.registry != nil {
		model = deps.registry.FullModelID(modelrole.RoleMain)
	}
	// Second-pass role resolution: fallback values (defaultModel, subagentDefaultModel,
	// sess.Model) may contain role names like "main" that need registry resolution.
	if deps.registry != nil && model != "" {
		if resolved, role, ok := deps.registry.ResolveModel(model); ok {
			model = resolved
			initialRole = role
		}
	}
	// Parse provider prefix (e.g., "google/gemini-3.0-flash" → provider="google", model="gemini-3.0-flash").
	providerID, modelName := modelrole.ParseModelID(model)
	model = modelName

	// Sub-agent provider remapping: if this session was spawned by another
	// agent and a "<provider>-subagent" config exists, use the alternate
	// API key. This allows main and sub-agents to use different accounts
	// on the same provider (separate rate limits).
	if sess != nil && sess.SpawnedBy != "" && providerID != "" && shouldRemapSubagentProvider(initialRole, providerID) {
		alt := providerID + "-subagent"
		if deps.providerConfigs != nil {
			if _, ok := deps.providerConfigs[alt]; ok {
				providerID = alt
			}
		}
	}

	return modelResolution{
		model:       model,
		providerID:  providerID,
		initialRole: initialRole,
	}
}

func shouldRemapSubagentProvider(role modelrole.Role, providerID string) bool {
	if role != modelrole.RoleCoding {
		return true
	}
	switch providerID {
	case "kimi", "mimo", "mimo-plan":
		return false
	default:
		return true
	}
}
