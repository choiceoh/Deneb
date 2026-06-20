package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
)

// ChatManager groups the chat pipeline and its channel delivery backends.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type ChatManager struct {
	chatHandler     *chat.Handler
	toolDeps        *chat.CoreToolDeps
	modelRegistry   *modelrole.Registry
	localAIHub      *localai.Hub
	embeddingClient *embedding.Client

	// fileStore is the one shared on-box file store (filestore) used by the
	// miniapp.files.* RPCs, the chat files tool, and the semantic reindex task —
	// a single instance so they all see the same root and the index stays in
	// sync. nil when the store can't be opened (the features degrade).
	fileStore filestore.Store
	// fileSemanticIndex is the BGE-M3 vector index over fileStore, maintained by
	// a background PeriodicTask (filestore_semindex.go). nil when disabled.
	fileSemanticIndex *filestore.SemanticIndex

	// proactiveRelay delivers agent-initiated messages (cron results,
	// gmail poll summaries, wiki dreaming notifications) to the user's
	// channel without routing through the LLM. The body is sent verbatim
	// and mirrored into the session transcript so follow-up user turns
	// retain context. Set in registerSessionRPCMethods once the
	// transcript store is available.
	proactiveRelay proactiveRelayDeps
}
