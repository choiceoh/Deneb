package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/aibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/pipebind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/platbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcbind"
)

// ChatManager groups the chat pipeline and its channel delivery backends.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type ChatManager struct {
	chatHandler     *pipebind.Handler
	toolDeps        *pipebind.CoreToolDeps
	modelRegistry   *aibind.ModelRoleRegistry
	localAIHub      *aibind.Hub
	embeddingClient *aibind.EmbeddingClient

	// fileSemindex owns the shared on-box store and semantic sidecar.
	fileSemindex *svcbind.FileSemIndexService

	// proactiveRelay delivers agent-initiated messages (cron results,
	// gmail poll summaries, wiki dreaming notifications) to the user's
	// channel without routing through the LLM. The body is sent verbatim
	// and mirrored into the session transcript so follow-up user turns
	// retain context. Set in registerSessionRPCMethods once the
	// transcript store is available.
	proactiveRelay svcbind.Relay

	// externalMCP holds one shared client per configured external MCP server
	// (DENEB_MCP_SERVERS), keyed by server name. Populated synchronously at
	// boot in initExternalMCPTools and read-only afterwards, so consumers
	// (chat tool bridges today; ingest tasks tomorrow) can grab a client via
	// ExternalMCPClient without a lock and share the same child process.
	externalMCP map[string]*platbind.MCPClient
}

// ExternalMCPClient returns the shared client for a configured external MCP
// server, or nil when that server is not configured. The client is safe for
// concurrent use; discovery state does not matter (calls lazily (re)spawn).
func (m *ChatManager) ExternalMCPClient(name string) *platbind.MCPClient {
	return m.externalMCP[name]
}
