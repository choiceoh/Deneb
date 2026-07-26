package toolwire

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/wire"
)

// RegisterCoreTools populates the tool registrar with all core agent tools.
func RegisterCoreTools(registry wire.ToolRegistrar, deps *wire.CoreToolDeps) {
	wire.RegisterCoreTools(registry, deps)
}

func RegisterFileTools(registry wire.ToolRegistrar, workspaceDir string, skillsCatalogDirs ...string) {
	wire.RegisterFileTools(registry, workspaceDir, skillsCatalogDirs...)
}

func RegisterCalendarTool(registry wire.ToolRegistrar, calDeps *wire.CalendarDeps) {
	wire.RegisterCalendarTool(registry, calDeps)
}

func RegisterContactsTool(registry wire.ToolRegistrar, contactsDeps *wire.ContactsDeps) {
	wire.RegisterContactsTool(registry, contactsDeps)
}

func RegisterWikiTools(registry wire.ToolRegistrar, wikiDeps *wire.WikiDeps, workspaceDir string, sessionCacheFlush wire.SessionCacheFlushFn) {
	wire.RegisterWikiTools(registry, wikiDeps, workspaceDir, sessionCacheFlush)
}

func RegisterPersonaTools(registry wire.ToolRegistrar, workspaceDir string) {
	wire.RegisterPersonaTools(registry, workspaceDir)
}

func RegisterNotebookTool(registry wire.ToolRegistrar, deps *wire.NotebookDeps) {
	wire.RegisterNotebookTool(registry, deps)
}

func RegisterSkillsTools(registry wire.ToolRegistrar, getSnapshot wire.SkillsSnapshotProvider, workspaceDir, bundledSkillsDir string, invalidateCache wire.SkillManageInvalidateFn) {
	wire.RegisterSkillsTools(registry, getSnapshot, workspaceDir, bundledSkillsDir, invalidateCache)
}

func RegisterProcessTools(registry wire.ToolRegistrar, d *wire.ProcessDeps) {
	wire.RegisterProcessTools(registry, d)
}

func RegisterSessionTools(registry wire.ToolRegistrar, d *wire.SessionDeps) {
	wire.RegisterSessionTools(registry, d)
}

func RegisterChronoTools(registry wire.ToolRegistrar) {
	wire.RegisterChronoTools(registry)
}

func RegisterMediaTools(registry wire.ToolRegistrar, workspaceDir string, spill wire.SpilloverStore) {
	wire.RegisterMediaTools(registry, workspaceDir, spill)
}

func RegisterWebTools(registry wire.ToolRegistrar, spill wire.SpilloverStore) {
	wire.RegisterWebTools(registry, spill)
}
