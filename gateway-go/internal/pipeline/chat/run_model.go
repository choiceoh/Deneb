package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
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
//  2. Sub-agent: session.Model → subagentDefaultModel
//  3. defaultModel from config
//  4. Registry main role fallback
//  5. Second-pass role resolution for fallback values
//  6. Provider prefix extraction ("google/gemini" → provider="google")
//  7. Sub-agent provider remapping ("<provider>-subagent")
func resolveModel(
	params RunParams,
	deps runDeps,
	sess *session.Session,
) modelResolution {
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
	if model == "" && sess != nil && sess.SpawnedBy != "" {
		// Sub-agent: use explicit session model if set at spawn time,
		// otherwise fall back to the configured subagent default model.
		if sess.Model != "" {
			model = sess.Model
		} else if deps.subagentDefaultModel != "" {
			model = deps.subagentDefaultModel
		}
	}
	if model == "" {
		model = deps.defaultModel
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
	if sess != nil && sess.SpawnedBy != "" && providerID != "" {
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
