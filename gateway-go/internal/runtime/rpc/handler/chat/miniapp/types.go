package miniapp

import (
	"context"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ChatHandler is the narrow chat pipeline surface consumed by the native
// miniapp bridge. It intentionally excludes async chat.send/abort/steer so the
// native bridge does not share the standard chat.* RPC dependency contract.
type ChatHandler interface {
	chatport.SyncRunner
	History(context.Context, *protocol.RequestFrame) *protocol.ResponseFrame
}

// WorkFeedStore is the native bridge's work-feed persistence port.
type WorkFeedStore interface {
	Append(workfeed.Item) (workfeed.Item, error)
	List(limit int, includeAcked bool) ([]workfeed.Item, int, error)
	// Correct annotates a card with a user correction (native long-press
	// feedback) and returns the updated item; used by miniapp.workfeed.feedback.
	Correct(id, note string) (workfeed.Item, error)
	// Rewrite replaces a card's body with a regenerated analysis and returns
	// the updated item; used by miniapp.workfeed.rewrite.
	Rewrite(id, newBody string) (workfeed.Item, error)
}

// Deps holds the dependencies for the native-client miniapp chat bridge.
type Deps struct {
	Chat ChatHandler
	// OcrImage OCRs a directly-shared image (native-client image capture).
	// Optional; nil disables miniapp.capture.image.
	OcrImage func(ctx context.Context, img []byte) (string, error)
	// DescribeImage understands a directly-shared image via the vision-capable
	// model chain (main model -> dedicated vision model), falling back to OcrImage
	// glyph extraction when no vision model is available or the describe fails.
	// Optional; nil keeps the OCR-only image path.
	DescribeImage func(ctx context.Context, img []byte, mime string) string
	// Transcribe transcribes a directly-shared audio recording (native-client
	// voice/meeting capture) via the ASR sidecar. hotwords is an optional
	// proper-noun bias list. Optional; nil disables miniapp.capture.audio.
	Transcribe func(ctx context.Context, audio []byte, mimeType, hotwords string) (string, error)
	// ExtractDocument extracts readable text from a directly-shared document's raw
	// bytes. Optional; nil disables miniapp.capture.document.
	ExtractDocument func(ctx context.Context, data []byte, filename, mimeType string) string
	// DigestOversized condenses an oversized extracted document before it enters
	// the agent turn. Optional; nil injects text unbounded.
	DigestOversized func(ctx context.Context, name, text, sourcePath string, sourceBodyLine int) string
	// SummarizePreview is unused: batch/document turns are path pointers, and
	// putting a summary in the user message re-parsed the attachment into chat.
	// Kept on Deps so method_registry wiring does not churn.
	SummarizePreview func(ctx context.Context, name, text string) string
	// Translate translates web-page text segments for the in-app browser's
	// in-place translation. Optional; nil disables miniapp.web.translate.
	Translate func(ctx context.Context, segments []string, targetLang string) ([]string, error)
	// Hotwords supplies proper-noun bias for audio-capture transcription.
	// Optional; nil or "" means no bias.
	Hotwords func() string
	// SaveCapture durably stores raw captured content and returns the stored
	// file's memory-relative path, absolute path, and 1-based body line.
	// Optional; nil skips persistence.
	SaveCapture func(kind, context, text string) (rel, abs string, bodyStartLine int, err error)
	// EnrichContacts merges a shared address book into existing wiki person pages.
	// Optional; nil disables the wiki enrichment bonus.
	EnrichContacts func(contactsJSON []byte) (wiki.ContactEnrichResult, error)
	// SaveContacts mirrors the synced address book into the contacts store.
	// Optional; nil disables the store write.
	SaveContacts func(contactsJSON []byte) (int, error)
	// WorkFeed records native-client inputs as actionable work-feed items and
	// provides today's feed context for miniapp.chat.send.
	WorkFeed WorkFeedStore
	// PublishDeliverable files a document-analysis result as a proper
	// doc_analysis work-feed card. Nil keeps the raw capture card fallback.
	PublishDeliverable func(text string) (bool, error)
	// IngestEvent queues a proactive judgment turn for a phone event.
	// Optional; nil disables miniapp.event.ingest.
	IngestEvent func(eventType, source, text string)
}
