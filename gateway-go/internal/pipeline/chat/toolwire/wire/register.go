// Package wire composes toolwire/core, ops, domain, and chrono registrations.
// The parent toolwire facade imports this package instead of the registration leaves
// so two-hop reach stays within Health Bench soft limits.
package wire

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/wikitool"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/chrono"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/core"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/domain"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/media"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/ops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/webtools"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/workflow"
)

// RegisterCoreTools populates the tool registrar with all core agent tools.
func RegisterCoreTools(registry toolport.ToolRegistrar, deps *tooldeps.CoreToolDeps) {
	core.Register(registry, deps)
	ops.RegisterRuntimeOps(registry, ops.RuntimeOpsDeps{
		WorkspaceDir:   deps.WorkspaceDir,
		GatewayVersion: deps.GatewayVersion,
		ObserveTool:    deps.ObserveTool,
		Fleet:          deps.Fleet,
		Browser:        deps.Browser,
		WikiStore:      deps.Wiki.Store,
		SpilloverStore: deps.SpilloverStore,
		SpilloverAsk:   deps.SpilloverAsk,
	})
	ops.RegisterProcessTools(registry, &deps.Process)
	webtools.Register(registry, deps.SpilloverStore)
	ops.RegisterSessionTools(registry, &deps.Sessions)
	ops.RegisterHeartbeatTool(registry)
	media.RegisterMediaTools(registry, deps.WorkspaceDir, deps.SpilloverStore)
	ops.RegisterPhoneTools(registry, deps.PhoneActionSender)
	ops.RegisterWorkstationTool(registry, ops.WorkstationDeps{
		Send: deps.WorkstationCommandSender,
		Hint: deps.WorkstationUsageHint,
	})
	media.RegisterExtractionTools(registry, deps.AsrHotwords)
	workflow.Register(registry)
	domain.Register(registry, deps)
	chrono.Register(registry, deps)
}

func RegisterFileTools(registry toolport.ToolRegistrar, workspaceDir string, skillsCatalogDirs ...string) {
	core.RegisterFileTools(registry, workspaceDir, skillsCatalogDirs...)
}

func RegisterProcessTools(registry toolport.ToolRegistrar, d *tooldeps.ProcessDeps) {
	ops.RegisterProcessTools(registry, d)
}

func RegisterSessionTools(registry toolport.ToolRegistrar, d *tooldeps.SessionDeps) {
	ops.RegisterSessionTools(registry, d)
}

func RegisterChronoTools(registry toolport.ToolRegistrar) {
	core.RegisterChronoTools(registry)
	ops.RegisterHeartbeatTool(registry)
}

func RegisterMediaTools(registry toolport.ToolRegistrar, workspaceDir string, spill tooldeps.SpilloverStore) {
	media.RegisterMediaTools(registry, workspaceDir, spill)
}

func RegisterWebTools(registry toolport.ToolRegistrar, spill tooldeps.SpilloverStore) {
	webtools.Register(registry, spill)
}

func RegisterCalendarTool(registry toolport.ToolRegistrar, calDeps *tooldeps.CalendarDeps) {
	chrono.RegisterCalendarTool(registry, calDeps)
}

func RegisterContactsTool(registry toolport.ToolRegistrar, contactsDeps *tooldeps.ContactsDeps) {
	domain.RegisterContactsTool(registry, contactsDeps)
}

func RegisterWikiTools(registry toolport.ToolRegistrar, wikiDeps *tooldeps.WikiDeps, workspaceDir string, sessionCacheFlush SessionCacheFlushFn) {
	domain.RegisterWikiTools(registry, wikiDeps, workspaceDir, sessionCacheFlush)
}

func RegisterPersonaTools(registry toolport.ToolRegistrar, workspaceDir string) {
	domain.RegisterPersonaTools(registry, workspaceDir)
}

func RegisterNotebookTool(registry toolport.ToolRegistrar, deps *tooldeps.NotebookDeps) {
	domain.RegisterNotebookTool(registry, deps)
}

func RegisterSkillsTools(registry toolport.ToolRegistrar, getSnapshot domain.SkillsSnapshotProvider, workspaceDir, bundledSkillsDir string, invalidateCache domain.SkillManageInvalidateFn) {
	domain.RegisterSkillsTools(registry, getSnapshot, workspaceDir, bundledSkillsDir, invalidateCache)
}

func ToolMaxOutputs() map[string]int { return domain.ToolMaxOutputs() }

type (
	SkillsSnapshotProvider  = domain.SkillsSnapshotProvider
	SkillManageInvalidateFn = domain.SkillManageInvalidateFn
)

// NewGoalGlanceFunc builds the ambient standing-goal glance.
func NewGoalGlanceFunc() func(ctx context.Context, sessionKey string) string {
	return workflow.NewGoalGlanceFunc()
}

// HandleGoalCommand processes the /goal slash command.
func HandleGoalCommand(sessionKey, args string, respond func(text string)) {
	workflow.HandleGoalCommand(sessionKey, args, respond)
}

// Type aliases so the toolwire facade can avoid importing toolport/tooldeps.
type (
	ToolRegistrar       = toolport.ToolRegistrar
	CoreToolDeps        = tooldeps.CoreToolDeps
	ProcessDeps         = tooldeps.ProcessDeps
	SessionDeps         = tooldeps.SessionDeps
	CalendarDeps        = tooldeps.CalendarDeps
	ContactsDeps        = tooldeps.ContactsDeps
	WikiDeps            = tooldeps.WikiDeps
	NotebookDeps        = tooldeps.NotebookDeps
	SpilloverStore      = tooldeps.SpilloverStore
	SessionCacheFlushFn = wikitool.SessionCacheFlushFn
)
