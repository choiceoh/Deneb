// Package chat provides RPC method handlers for the chat domain.
//
// It exposes Methods and BtwMethods, which return handler maps that can be
// bulk-registered on the rpc.Dispatcher.
package chat

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// BroadcastFunc is the canonical broadcast type defined in rpcutil.
type BroadcastFunc = rpcutil.BroadcastFunc

// ChatHandler is the protocol and synchronous-run surface consumed by this RPC
// domain. The concrete chat pipeline implements it at the composition root.
type ChatHandler interface {
	chatport.SyncRunner
	Send(context.Context, *protocol.RequestFrame) *protocol.ResponseFrame
	History(context.Context, *protocol.RequestFrame) *protocol.ResponseFrame
	Abort(context.Context, *protocol.RequestFrame) *protocol.ResponseFrame
	EnqueueSteer(sessionKey, note string) bool
}

// Deps holds the dependencies for standard chat RPC methods (send, history, abort, steer).
type Deps struct {
	Chat        ChatHandler
	Broadcaster BroadcastFunc // optional; receives chat.steer_received events
	// OcrImage OCRs a directly-shared image (native-client image capture).
	// Optional; nil disables miniapp.capture.image.
	OcrImage func(ctx context.Context, img []byte) (string, error)
	// Transcribe transcribes a directly-shared audio recording (native-client
	// voice/meeting capture) via the ASR sidecar (MOSS-Transcribe-Diarize). hotwords is an
	// optional proper-noun bias list. Optional; nil disables miniapp.capture.audio.
	Transcribe func(ctx context.Context, audio []byte, mimeType, hotwords string) (string, error)
	// ExtractDocument extracts readable text from a directly-shared document's raw
	// bytes — PDF/Excel/Word/PowerPoint/CSV/text, with a scanned-PDF / image OCR
	// fallback (the native-client document attach path). Optional; nil disables
	// miniapp.capture.document.
	ExtractDocument func(ctx context.Context, data []byte, filename, mimeType string) string
	// DigestOversized condenses an oversized extracted document (a line-mapped
	// chunk-digest map via the local lightweight model, or a visible head
	// truncation) before it enters the agent turn. Runs AFTER raw capture
	// persistence so the full original still lands in captures; sourcePath/
	// sourceBodyLine locate that archived file so the map's line numbers stay
	// openable ("" / 0 when persistence failed). Optional; nil injects text
	// unbounded.
	DigestOversized func(ctx context.Context, name, text, sourcePath string, sourceBodyLine int) string
	// SummarizePreview condenses one attachment's extracted text into a compact
	// (~1000자) local tiny-model summary for the batch-capture pointer turn — a
	// representative preview instead of a raw front-of-text cut. Returns "" when
	// the local model is unavailable or the summary fails; the batch handler then
	// falls back to its own front-cut preview. Optional; nil keeps the front-cut.
	SummarizePreview func(ctx context.Context, name, text string) string
	// Translate translates web-page text segments for the in-app browser's
	// in-place translation (en/ru → ko). Returns a same-length, same-order
	// slice. Optional; nil disables miniapp.web.translate.
	Translate func(ctx context.Context, segments []string, targetLang string) ([]string, error)
	// Hotwords supplies proper-noun bias (wiki people/companies/domain terms)
	// for audio-capture transcription. Optional; nil or "" means no bias.
	Hotwords func() string
	// SaveCapture durably stores raw captured content (full ASR transcript,
	// OCR text) and returns the stored file's memory-relative path, absolute
	// path, and the 1-based line where the body starts inside the file. The
	// agent turn only summarizes; without this the original is unrecoverable
	// once the chat transcript ages out. Optional; nil skips persistence.
	SaveCapture func(kind, context, text string) (rel, abs string, bodyStartLine int, err error)
	// EnrichContacts merges a shared address book into EXISTING wiki 사람 pages —
	// it creates no pages, only enriches people already in the wiki with their
	// phone/email/org (native-client contacts sync). Optional; nil disables the
	// wiki enrichment bonus (the contacts store save is the primary path).
	EnrichContacts func(contactsJSON []byte) (wiki.ContactEnrichResult, error)
	// SaveContacts mirrors the synced address book into the contacts store (phone
	// lookup, name search, ASR hotwords). Optional; nil disables the store write.
	SaveContacts func(contactsJSON []byte) (int, error)
	// WorkFeed records native-client inputs as actionable work-feed items, and
	// (List) reads them back so a 업무 chat turn can inject today's feed as
	// context — see handleMiniappChatSend. Optional; capture handlers ignore
	// append failures because the chat turn is still the source of truth, and a
	// nil store just means no feed context is injected.
	WorkFeed interface {
		Append(workfeed.Item) (workfeed.Item, error)
		List(limit int, includeAcked bool) ([]workfeed.Item, int, error)
		// Correct annotates a card with a user correction (native long-press
		// feedback) and returns the updated item; used by miniapp.workfeed.feedback.
		Correct(id, note string) (workfeed.Item, error)
		// Rewrite replaces a card's body with a regenerated analysis and returns
		// the updated item; used by miniapp.workfeed.rewrite.
		Rewrite(id, newBody string) (workfeed.Item, error)
	}
	// PublishDeliverable files a document-analysis result as a proper doc_analysis
	// work-feed card (derived title, substance-gated) instead of a raw capture card.
	// Returns (true, nil) when a card was filed; (false, nil) when the analysis was
	// too thin to be a deliverable — the caller then falls back to the raw capture
	// card so a shared document is never dropped. Wired to the proactive relay's card
	// builder; nil keeps the legacy raw capture card.
	PublishDeliverable func(text string) (bool, error)
	// IngestEvent queues a proactive 비서실장 judgment turn for a phone event — the
	// native NotificationListener's broad capture (the gateway triages: OTP/spam/
	// routine → NO_REPLY, signal → work feed + push). Optional; nil disables
	// miniapp.event.ingest. eventType is "notification"/"context"/"clipboard"/sms.
	IngestEvent func(eventType, source, text string)
}

// BtwDeps holds the dependencies for the chat.btw side-question RPC method.
type BtwDeps struct {
	// Chat is the native chat handler for processing side questions.
	Chat interface {
		// HandleBtw processes a side question and returns the answer text.
		// Returns empty string if the handler doesn't support /btw yet.
		HandleBtw(ctx context.Context, sessionKey, question string) (string, error)
	}
	Broadcaster BroadcastFunc
}

// Methods returns the standard chat RPC handlers keyed by method name.
// If deps.Chat is nil, an empty map is returned.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Chat == nil || !deps.Chat.ChatReady() {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"chat.send":    handleSend(deps),
		"chat.history": handleHistory(deps),
		"chat.abort":   handleAbort(deps),
		"chat.steer":   handleSteer(deps),
	}
}

// BtwMethods returns the chat.btw side-question RPC handler keyed by method name.
func BtwMethods(deps BtwDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"chat.btw": handleChatBtw(deps),
	}
}

// handleSend delegates to the chat handler's Send method.
func handleSend(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.Send(ctx, req)
	}
}

// handleHistory delegates to the chat handler's History method.
func handleHistory(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.History(ctx, req)
	}
}

// handleAbort delegates to the chat handler's Abort method.
func handleAbort(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.Abort(ctx, req)
	}
}

// handleSteer queues a /steer note for the main agent's next tool_result
// without interrupting the active run.
//
// Params:
//   - sessionKey (string, required): target session
//   - note       (string, required): user nudge text (trimmed)
//
// On accept, broadcasts "chat.steer_received" so the UI can surface the
// pending nudge. The note is drained and injected by the running agent
// goroutine right before its next LLM call.
func handleSteer(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			SessionKey string `json:"sessionKey"`
			Note       string `json:"note"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.SessionKey == "" {
			return rpcerr.MissingParam("sessionKey").Response(req.ID)
		}
		if p.Note == "" {
			return rpcerr.MissingParam("note").Response(req.ID)
		}
		if deps.Chat == nil {
			return rpcerr.Unavailable("chat handler not available").Response(req.ID)
		}
		accepted := deps.Chat.EnqueueSteer(p.SessionKey, p.Note)
		if !accepted {
			// Empty after trim, or queue unavailable. Surface as invalid
			// rather than silently swallowing so the caller notices.
			return rpcerr.InvalidRequest("steer note is empty").Response(req.ID)
		}
		if deps.Broadcaster != nil {
			wire, _ := events.PayloadOf(map[string]any{
				"sessionKey": p.SessionKey,
				"note":       p.Note,
			})
			_, _ = deps.Broadcaster("chat.steer_received", wire)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":         true,
			"sessionKey": p.SessionKey,
		})
	}
}

// handleChatBtw processes a side question without affecting the main session
// context. In native Go mode, this routes through the chat handler directly.
//
// Params:
//   - question (string, required): The side question to answer.
//   - sessionKey (string, required): The active session key.
//
// Returns the side question answer text, and broadcasts a chat.side_result event.
func handleChatBtw(deps BtwDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Question   string `json:"question"`
			SessionKey string `json:"sessionKey"`
		}](req)
		if errResp != nil {
			return errResp
		}
		p.Question = strings.TrimSpace(p.Question)
		p.SessionKey = strings.TrimSpace(p.SessionKey)
		if p.Question == "" {
			return rpcerr.MissingParam("question").Response(req.ID)
		}
		if p.SessionKey == "" {
			return rpcerr.MissingParam("sessionKey").Response(req.ID)
		}

		if deps.Chat == nil {
			return rpcerr.Unavailable("chat handler not available").Response(req.ID)
		}

		// Process side question through native chat handler.
		text, err := deps.Chat.HandleBtw(ctx, p.SessionKey, p.Question)
		if err != nil {
			return rpcerr.WrapDependencyFailed("btw failed", err).Response(req.ID)
		}

		// Broadcast side_result event to connected clients.
		if deps.Broadcaster != nil {
			wire, _ := events.PayloadOf(map[string]any{
				"kind":       "btw",
				"sessionKey": p.SessionKey,
				"question":   p.Question,
				"text":       text,
			})
			_, _ = deps.Broadcaster("chat.side_result", wire)
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"text": text,
		})
	}
}
