// Package wire composes toolwire/core, domain, and chrono registrations.
// The parent toolwire facade imports this package instead of the three leaves
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
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/webtools"
)

// RegisterCoreTools populates the tool registrar with all core agent tools.
func RegisterCoreTools(registry toolport.ToolRegistrar, deps *tooldeps.CoreToolDeps) {
	core.Register(registry, deps)
	domain.Register(registry, deps)
	chrono.Register(registry, deps)
}

func RegisterFileTools(registry toolport.ToolRegistrar, workspaceDir string, skillsCatalogDirs ...string) {
	core.RegisterFileTools(registry, workspaceDir, skillsCatalogDirs...)
}

func RegisterProcessTools(registry toolport.ToolRegistrar, d *tooldeps.ProcessDeps) {
	core.RegisterProcessTools(registry, d)
}

func RegisterSessionTools(registry toolport.ToolRegistrar, d *tooldeps.SessionDeps) {
	core.RegisterSessionTools(registry, d)
}

func RegisterChronoTools(registry toolport.ToolRegistrar) {
	core.RegisterChronoTools(registry)
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
	return core.NewGoalGlanceFunc()
}

// HandleGoalCommand processes the /goal slash command.
func HandleGoalCommand(sessionKey, args string, respond func(text string)) {
	core.HandleGoalCommand(sessionKey, args, respond)
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
