package chat

import "context"

// LinkEnrichStart starts async URL enrichment for a turn. The composition root
// (server) wires the concrete linkenrichment engine so chat does not import it.
type LinkEnrichStart func(ctx context.Context, message string, sanitize func(string) string) func(context.Context) string

// startLinkEnrichment preserves chat-owned eligibility gates.
func (h *Handler) startLinkEnrichment(ctx context.Context, message string, opts *SyncOptions) func(context.Context) string {
	if h != nil && h.briefcaseMode {
		return nil
	}
	if opts != nil && len(opts.Messages) > 0 {
		return nil
	}
	if h == nil || h.linkEnrichStart == nil {
		return nil
	}
	return h.linkEnrichStart(ctx, message, sanitizeInput)
}
