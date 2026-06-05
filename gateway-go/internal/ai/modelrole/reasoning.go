package modelrole

// IsReasoningModel reports whether model denotes a reasoning model — one that
// generates a separate chain-of-thought channel, surfaced as reasoning_content
// on OpenAI-compatible (vLLM) streaming responses. Backed by the model Profile
// (profile.go) so reasoning detection stays consistent with sampling tuning;
// add or adjust a model in ProfileFor and every caller follows.
func IsReasoningModel(model string) bool {
	return ProfileFor(model).Reasoning
}

// RoleIsReasoning reports whether the model configured for role is a reasoning
// model. An unconfigured role (empty model name) is not a reasoning model.
func (r *Registry) RoleIsReasoning(role Role) bool {
	return IsReasoningModel(r.Model(role))
}
