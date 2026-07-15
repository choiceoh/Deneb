// Package chatwire holds expensive chat-tool wiring (toolwire, denebui,
// externalmcp, modelpanel, notebooksource, knowledge, polaris) so toolbind
// stays within Health Bench fan-out budget.
package chatwire

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/denebui"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/linkenrichment"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/schedule"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mcpclient"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/externalmcp"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/modelpanel"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/notebooksource"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind/docmedia"
)

// ReportCardHealth validates deneb-ui fences in the final reply (wired into
// chat.HandlerConfig by the server composition root).
var ReportCardHealth = denebui.ReportCardHealth

// NotebookFetchURL and NotebookReadMail are notebook source ingesters (url/mail).
var (
	NotebookFetchURL = notebooksource.FetchURL
	NotebookReadMail = notebooksource.ReadMail
)

// NotebookReadDiary wraps the wiki-backed diary ingester for notebook sources.
func NotebookReadDiary(wikiStore *wiki.Store) func(context.Context, string) (string, error) {
	return func(ctx context.Context, ref string) (string, error) {
		return notebooksource.ReadDiary(wikiStore, ref)
	}
}

// NewConsultPanel wires the research_panel fan-out engine.
func NewConsultPanel(reg *modelrole.Registry, logger *slog.Logger) func(context.Context, string, string, []string) []tooldeps.PanelAnswer {
	return modelpanel.New(reg, logger).Consult
}

// StartExternalMCP discovers external MCP servers as deferred chat tools.
func StartExternalMCP(shutdownCtx context.Context, registry toolport.ToolRegistrar, logger *slog.Logger) map[string]*mcpclient.Client {
	return externalmcp.Start(shutdownCtx, registry, logger)
}

// FileKnowledgeSource optionally supplies a files-layer adapter for the knowledge router.
type FileKnowledgeSource interface {
	KnowledgeAdapter() knowledge.Adapter
}

// WireKnowledgeTool builds the knowledge router and registers the knowledge tool.
// Returns true when a files adapter is live (caller should wire FileRecall).
func WireKnowledgeTool(registry toolport.ToolRegistrar, wikiStore *wiki.Store, files FileKnowledgeSource) bool {
	var filesAdapter knowledge.Adapter
	if files != nil {
		filesAdapter = files.KnowledgeAdapter()
	}
	router := knowledge.New(
		knowledge.NewWikiAdapter(wikiStore),
		filesAdapter,
	)
	toolwire.RegisterKnowledgeTool(registry, router)
	return filesAdapter != nil
}

type LocalAIFunc = docmedia.LocalAIFunc

// RegisterPolarisTools registers the unified Polaris retrieval tool.
func RegisterPolarisTools(registry toolport.ToolRegistrar, store *polaris.Store, localAI LocalAIFunc) {
	toolwire.RegisterPolarisTools(registry, store, localAI)
}

// NewCalendarGlance wires the ambient calendar glance for chat.HandlerConfig.
func NewCalendarGlance(d *tooldeps.CalendarDeps) schedule.CalendarGlanceFunc {
	return schedule.NewCalendarGlanceFunc(d)
}

// NewLinkEnrichStart wires the concrete linkenrichment engine for chat.HandlerConfig.
func NewLinkEnrichStart(logger *slog.Logger) func(context.Context, string, func(string) string) func(context.Context) string {
	engine := linkenrichment.New(linkenrichment.Config{Logger: logger})
	return func(ctx context.Context, message string, sanitize func(string) string) func(context.Context) string {
		return engine.Start(ctx, message, sanitize)
	}
}
