package llm

// ThinkingOffFields returns the request-body fields that turn a model's
// reasoning off, for the raw-call adapters that shape a request's ExtraBody.
// modelrole decides WHETHER and HOW (a typed directive); this is the one place
// that knows what each "how" looks like on the wire:
//
//   - a chat-template kwarg, for vLLM-style servings:
//     {"chat_template_kwargs": {<kwarg>: false}}
//   - OpenRouter's unified reasoning switch, which works whatever template sits
//     underneath: {"reasoning": {"enabled": false}}
//
// Nil when neither applies. The template kwarg wins when both are given: it is
// the more specific instruction.
func ThinkingOffFields(templateKwarg string, reasoningParam bool) map[string]any {
	switch {
	case templateKwarg != "":
		return map[string]any{"chat_template_kwargs": map[string]any{templateKwarg: false}}
	case reasoningParam:
		return map[string]any{"reasoning": map[string]any{"enabled": false}}
	default:
		return nil
	}
}
