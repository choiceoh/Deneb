package knowledge

import "context"

// readAttributionKey carries the session label a model-driven read surface
// attaches to the context. Owned by this package so setting it never makes a
// caller import chat internals, and the domain never reads chat context keys.
type readAttributionKey struct{}

// WithReadAttribution marks ctx as a MODEL-DRIVEN read surface attributed to
// the given session. Adapters record usage telemetry (the wiki recall-utility
// ledger's read events) only for attributed reads: an unattributed Read is a
// mechanical consumer (enrichment, background jobs) whose access is not
// evidence that recalled knowledge was USED, and a session-less read event
// cannot be joined against the inject exposure it should credit.
func WithReadAttribution(ctx context.Context, session string) context.Context {
	if session == "" {
		return ctx
	}
	return context.WithValue(ctx, readAttributionKey{}, session)
}

// ReadAttribution returns the attributed session and whether one is set.
func ReadAttribution(ctx context.Context) (string, bool) {
	session, ok := ctx.Value(readAttributionKey{}).(string)
	return session, ok && session != ""
}

// injectAttribution labels a model-visible EXPOSURE surface: an adapter whose
// Recall results are rendered into model context records inject events for
// them when this is set. The chat recall preflight does NOT set it — it
// records its own richer inject lines (rank, score, gate-shadow signals) at
// the ledger directly; this path exists for the secondary surfaces (mail
// archive enrichment, briefings) whose exposure previously went uncounted.
type injectAttributionKey struct{}

type injectAttribution struct {
	session string
	label   string
}

// WithInjectAttribution marks ctx as a model-visible exposure surface.
func WithInjectAttribution(ctx context.Context, session, label string) context.Context {
	if label == "" {
		return ctx
	}
	return context.WithValue(ctx, injectAttributionKey{}, injectAttribution{session: session, label: label})
}

// InjectAttribution returns the exposure surface attribution when set.
func InjectAttribution(ctx context.Context) (session, label string, ok bool) {
	a, ok := ctx.Value(injectAttributionKey{}).(injectAttribution)
	return a.session, a.label, ok && a.label != ""
}
