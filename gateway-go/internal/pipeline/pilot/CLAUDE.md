# Local-model pilot change map

Pilot is the small adapter layer that resolves model roles and consumes local
LLM streams for lightweight text and vision helpers. Business pipelines call a
role; provider selection and health fallback remain centralized here.

## Entry points

- `localai.go`: `CallRoleLLM` is the typed role-based entry, while
  `CallLocalLLM` and `CallTinyLLM` are compatibility wrappers.
- `CollectStream` owns delta/error/cancellation parsing and `ExtractDeltaText`
  owns supported provider event shapes.
- `SetModelRoleRegistry` and `SetLocalAIHub` install process-level adapters;
  `LocalAIRecentlyDown` is the synchronized degradation signal.
- `vision.go`: `VisionFrame` and `CallVisionLLM` validate and assemble
  multimodal requests through the same role registry.

## Dependency direction and invariants

- Pilot may depend on AI provider/model-role packages, never on runtime server,
  chat handler, or domain consumers.
- Callers select a role, not a concrete model. Registry resolution and fallback
  order are the single source of truth. Candidates after the role's own model come
  from `Registry.HelperFallbacks` — never walk `FallbackChain` directly here: that
  skips the billing rule (`SkipBilledFallback`), and it did — 2,262 tiny calls in
  30 days landed on a metered model before the fix.
- Extra request bodies merge without discarding earlier keys. Stream errors are
  returned even when partial text exists; cancellation remains attributable to
  the caller context.
- `stream_retry.go`: an error reported INSIDE a 200 stream is a `StreamError`
  carrying the provider's numeric code. Before any text, a 5xx is retried on the
  same model twice (short spacing) and then the fallback chain moves on; a 429
  moves on at once. A code-less error, or one after partial text, surfaces as
  before — wording is not trusted to mean "transient". OpenRouter's free tier
  reports upstream overload exactly this way (HTTP 200 + in-stream error), which
  the LLM client's HTTP-status retries never see. The decision itself is
  `llm.ClassifyInBandError`, shared with mail stage-1 extraction — change it
  there, not here.
- Model hub and degradation state are race-safe. Vision rejects empty or
  malformed frames before contacting a provider.
- Output truncation is rune-safe and preserves the leading diagnostic context.

## Focused verification

Use `contracts_test.go` for role, fallback, state, and vision contracts and
`localai_test.go` for stream parsing.

`cd gateway-go && go test -count=1 ./internal/pipeline/pilot`
