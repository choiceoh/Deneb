// link_enrichment.go bridges Handler turn policy to the link-enrichment
// subsystem. URL extraction, fetch/conversion, budgets, and async join
// lifecycle are owned by the linkenrichment package.
package chat

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/linkenrichment"
)

// startLinkEnrichment preserves the chat-owned eligibility gates: signed
// briefcase turns and API calls carrying caller-owned history must remain
// byte-for-byte untouched. Eligible interactive messages delegate their
// asynchronous fetch/join lifecycle to the handler-scoped engine.
func (h *Handler) startLinkEnrichment(ctx context.Context, message string, opts *SyncOptions) linkenrichment.Join {
	if h != nil && h.briefcaseMode {
		return nil
	}
	if opts != nil && len(opts.Messages) > 0 {
		return nil
	}
	engine := (*linkenrichment.Engine)(nil)
	logger := slog.Default()
	if h != nil {
		engine = h.linkEnrichment
		if h.logger != nil {
			logger = h.logger
		}
	}
	if engine == nil {
		engine = linkenrichment.New(linkenrichment.Config{Logger: logger})
	}
	return engine.Start(ctx, message, sanitizeInput)
}
