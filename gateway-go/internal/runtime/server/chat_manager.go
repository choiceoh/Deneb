package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	airerank "github.com/choiceoh/deneb/gateway-go/internal/ai/rerank"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mcpclient"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/filesemindex"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
)

// ChatManager groups the chat pipeline and its channel delivery backends.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type ChatManager struct {
	chatHandler     *chat.Handler
	toolDeps        *chat.CoreToolDeps
	modelRegistry   *modelrole.Registry
	localAIHub      *localai.Hub
	embeddingClient *embedding.Client
	rerankerClient  *airerank.Client

	// fileSemindex owns the shared on-box store and semantic sidecar.
	fileSemindex *filesemindex.Service

	// proactiveRelay delivers agent-initiated messages (cron results,
	// gmail poll summaries, wiki dreaming notifications) to the user's
	// channel without routing through the LLM. The body is sent verbatim
	// and mirrored into the session transcript so follow-up user turns
	// retain context. Set in registerSessionRPCMethods once the
	// transcript store is available.
	proactiveRelay proactive.Relay

	// externalMCP holds one shared client per configured external MCP server
	// (DENEB_MCP_SERVERS), keyed by server name. Populated synchronously at
	// boot in initExternalMCPTools and read-only afterwards, so consumers
	// (chat tool bridges today; ingest tasks tomorrow) can grab a client via
	// ExternalMCPClient without a lock and share the same child process.
	externalMCP map[string]*mcpclient.Client
}

// ExternalMCPClient returns the shared client for a configured external MCP
// server, or nil when that server is not configured. The client is safe for
// concurrent use; discovery state does not matter (calls lazily (re)spawn).
func (m *ChatManager) ExternalMCPClient(name string) *mcpclient.Client {
	return m.externalMCP[name]
}
