// dreamer.go — WikiDreamer: implements autonomous.Dreamer for wiki-based
// memory consolidation. Instead of SQL-based fact verification/merging,
// it scans diary entries and synthesizes them into wiki pages via LLM.
package wiki

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// Dreaming configuration.
const (
	wikiDreamTurnThreshold = 50
	wikiDreamTimeIntervalH = 8
	wikiDreamTimeout       = 10 * time.Minute
	// wikiDreamSynthesisTimeout bounds the synthesis LLM call alone: a wedged
	// backend must fail the phase quickly instead of eating the whole cycle
	// budget (a stuck vLLM engine held every cycle for the full 10 minutes).
	wikiDreamSynthesisTimeout = 5 * time.Minute
	wikiDreamMaxTokens        = 4096
	diaryProcessStateFile     = ".diary-process-state.json"
	dreamProposalFile         = ".dream-last-proposal.json"
	processedCapsuleLimit     = 12
)

// Compile-time interface compliance.
var _ autonomous.Dreamer = (*WikiDreamer)(nil)

type diaryScanResult struct {
	Content    string
	State      diaryProcessState
	LatestDate string
}

type diaryProcessState struct {
	Version   int                       `json:"version"`
	Files     map[string]diaryFileState `json:"files"`
	Recent    []processedDiaryCapsule   `json:"recent,omitempty"`
	UpdatedAt string                    `json:"updatedAt,omitempty"`
	// LastDreamMs is the unix-millis time of the last dream cycle, persisted so
	// the 8h time-trigger survives gateway restarts (which happen every few
	// minutes). Without it, in-memory lastDream reset to zero on every boot and
	// dreaming never fired.
	LastDreamMs int64 `json:"lastDreamMs,omitempty"`
	// MemoryConsumedThrough is the high-water stamp ("YYYY-MM-DD HH:MM") of
	// workspace MEMORY.md sections already distilled into the wiki. Sections
	// at or before this stamp may be dropped from MEMORY.md once they age out
	// of the keep window (see memory_curation.go).
	MemoryConsumedThrough string `json:"memoryConsumedThrough,omitempty"`
}

type diaryFileState struct {
	Offset  int64 `json:"offset"`
	Size    int64 `json:"size,omitempty"`
	ModUnix int64 `json:"modUnix,omitempty"`
}

type processedDiaryCapsule struct {
	At        string   `json:"at"`
	DiaryDate string   `json:"diaryDate,omitempty"`
	Proposed  int      `json:"proposed"`
	Created   int      `json:"created"`
	Updated   int      `json:"updated"`
	Paths     []string `json:"paths,omitempty"`
}

type dreamProposalReport struct {
	GeneratedAt     string               `json:"generatedAt"`
	LatestDiaryDate string               `json:"latestDiaryDate,omitempty"`
	DiaryBytes      int                  `json:"diaryBytes"`
	Proposed        []dreamUpdatePreview `json:"proposed"`
	Applied         dreamApplySummary    `json:"applied,omitempty"`
	PhaseErrors     []string             `json:"phaseErrors,omitempty"`
	DurationMs      int64                `json:"durationMs,omitempty"`
}

type dreamUpdatePreview struct {
	Action      string   `json:"action"`
	Path        string   `json:"path"`
	Title       string   `json:"title,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Category    string   `json:"category,omitempty"`
	Type        string   `json:"type,omitempty"`
	Confidence  string   `json:"confidence,omitempty"`
	Importance  float64  `json:"importance,omitempty"`
	Related     []string `json:"related,omitempty"`
	ContentHint string   `json:"contentHint,omitempty"`
}

type dreamApplySummary struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
}

// WikiDreamer implements autonomous.Dreamer for wiki-based knowledge consolidation.
// Phases:
//  1. Scan unprocessed diary entries
//  2. LLM synthesis: identify which wiki pages to create/update
//  3. Apply page updates
//  4. Rebuild index
type WikiDreamer struct {
	store  *Store
	config Config
	client *llm.Client
	model  string
	logger *slog.Logger

	// cmu guards turnCount and lastDream: incremented from chat turns,
	// read from the autonomous dream timer loop, reset from async dream
	// runs — three goroutines on a plain int/time without it.
	cmu       sync.Mutex
	turnCount int
	lastDream time.Time

	// polarisContextFn optionally returns formatted recent polaris compression
	// summaries to inject into the synthesis prompt as a higher-density fact
	// source alongside raw diary entries. Wired by the chat pipeline; the wiki
	// package does not import polaris directly.
	polarisContextFn func() string

	// workspaceDir is the agent workspace containing MEMORY.md. Empty disables
	// memory curation (see memory_curation.go).
	workspaceDir string

	// openLoopSink receives unfulfilled commitments extracted each cycle
	// (see open_loops.go). nil disables the extraction pass.
	openLoopSink func(ctx context.Context, loops []OpenLoop) (int, error)

	// personDirectory supplies the address-book snapshot for mention-driven
	// 인물 page seeding (see person_seed.go). nil disables seeding.
	personDirectory func() []PersonSeed
}

// NewWikiDreamer creates a new wiki dreamer.
func NewWikiDreamer(store *Store, client *llm.Client, model string, cfg Config, logger *slog.Logger) *WikiDreamer {
	wd := &WikiDreamer{
		store:  store,
		config: cfg,
		client: client,
		model:  model,
		logger: logger,
	}
	// Restore lastDream from persisted state so the 8h time-trigger survives
	// gateway restarts. Without this, lastDream stayed zero on every boot,
	// ShouldDream's IsZero guard blocked the time path, and turnCount (also reset
	// on restart) rarely reached its threshold — dreaming was dead for ~26 days.
	// On the first run (no persisted value), seed lastDream=now and persist so
	// the interval starts counting from boot instead of staying zero forever.
	if store != nil {
		state := wd.loadDiaryProcessState()
		if state.LastDreamMs > 0 {
			wd.lastDream = time.UnixMilli(state.LastDreamMs)
		} else {
			wd.lastDream = time.Now()
			state.LastDreamMs = wd.lastDream.UnixMilli()
			if err := wd.saveDiaryProcessState(state); err != nil && logger != nil {
				logger.Warn("wiki-dream: seed lastDream failed", "error", err)
			}
		}
	}
	return wd
}

// IncrementTurn records a conversation turn for threshold tracking.
func (wd *WikiDreamer) IncrementTurn(_ context.Context) {
	wd.cmu.Lock()
	wd.turnCount++
	wd.cmu.Unlock()
}

// SetPolarisContextFn wires a closure that returns formatted recent polaris
// compression summaries. nil-safe; passing nil disables polaris injection.
func (wd *WikiDreamer) SetPolarisContextFn(fn func() string) {
	wd.polarisContextFn = fn
}

// SetWorkspaceDir wires the workspace directory so dream cycles can consume
// and curate the auto-recorded MEMORY.md (see memory_curation.go). Empty
// disables memory curation.
func (wd *WikiDreamer) SetWorkspaceDir(dir string) {
	wd.workspaceDir = dir
}

// ShouldDream checks if dreaming conditions are met.
func (wd *WikiDreamer) ShouldDream(_ context.Context) bool {
	wd.cmu.Lock()
	turns := wd.turnCount
	last := wd.lastDream
	wd.cmu.Unlock()

	if turns >= wikiDreamTurnThreshold {
		wd.logger.Info("wiki-dream: turn threshold reached", "turns", turns)
		return true
	}
	if !last.IsZero() && time.Since(last).Hours() >= float64(wikiDreamTimeIntervalH) {
		wd.logger.Info("wiki-dream: time threshold reached", "elapsed", time.Since(last).Round(time.Minute))
		return true
	}
	return false
}

// RunDream executes the wiki consolidation cycle.
func (wd *WikiDreamer) RunDream(ctx context.Context) (*autonomous.DreamReport, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, wikiDreamTimeout)
	defer cancel()

	report := &autonomous.DreamReport{}
	var phaseErrors []string

	// Phase 1: Scan unprocessed diary entries.
	scan, err := wd.scanDiaries(ctx)
	if err != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("diary-scan: %v", err))
	}
	if scan == nil {
		// No new diary bytes. Keep a state-bearing scan so a MEMORY.md-only
		// cycle flows through the same synthesis/persistence tail.
		scan = &diaryScanResult{State: wd.loadDiaryProcessState()}
	}
	diaryContent := scan.Content

	// Phase 1a: hard on-disk cap for MEMORY.md, enforced unconditionally before
	// any early return below. Phase 4b curation only runs when synthesis
	// consumes sections, so a disabled/lagging dreamer (no diary bytes, no LLM
	// client) would otherwise let the file grow without bound. This bounds it
	// regardless of dreaming health.
	if n, derr := wd.enforceMemoryDiskCap(); derr != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("memory-disk-cap: %v", derr))
	} else if n > 0 {
		wd.logger.Info("wiki-dream: MEMORY.md disk-capped", "droppedSections", n)
	}

	// Phase 1b: unconsumed workspace MEMORY.md sections join the synthesis
	// input — same distillation as diaries; the file is curated in Phase 4b.
	memScan := wd.scanWorkspaceMemory(scan.State.MemoryConsumedThrough)
	synthInput := diaryContent
	if memScan != nil {
		synthInput += memScan.Content
		wd.logger.Info("wiki-dream: memory sections queued for distillation",
			"sections", memScan.Sections, "through", memScan.ConsumedThrough)
	}

	if synthInput == "" {
		wd.logger.Info("wiki-dream: no new diary or memory entries to process")
		wd.resetCounters()
		report.DurationMs = time.Since(start).Milliseconds()
		return report, nil
	}

	// Phase 2: LLM synthesis — determine which wiki pages to update.
	//
	// Both failure paths below back off a full interval (resetCounters).
	// Without it, ShouldDream stays true and the 30-min timer hot-loops a
	// doomed cycle — with a wedged LLM each attempt burned the entire 10-min
	// cycle timeout, observed in production on 2026-06-11. Nothing is lost by
	// backing off: diary offsets and the MEMORY.md high-water mark only
	// persist on success, so the content is re-consumed next cycle.
	if wd.client == nil {
		phaseErrors = append(phaseErrors, "synthesis: LLM client not available")
		wd.resetCounters()
		report.PhaseErrors = phaseErrors
		report.DurationMs = time.Since(start).Milliseconds()
		return report, nil
	}

	updates, err := wd.synthesize(ctx, synthInput, scan.State)
	if err != nil {
		// Dreaming silently stalling is the audit's #1 ghost failure —
		// surface it at Error so the operator sees consolidation is stuck.
		wd.logger.Error("wiki-dream: synthesis failed; backing off one interval", "error", err)
		wd.resetCounters()
		phaseErrors = append(phaseErrors, fmt.Sprintf("synthesis: %v", err))
		report.PhaseErrors = phaseErrors
		report.DurationMs = time.Since(start).Milliseconds()
		return report, nil
	}
	report.WikiUpdatesProposed = len(updates)
	proposal := buildDreamProposalReport(scan, updates)
	proposalPath := wd.dreamProposalPath()
	report.WikiProposalPath = proposalPath
	if err := wd.saveDreamProposalReport(proposal); err != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("proposal-save: %v", err))
	}

	// Phase 3: Apply page updates.
	created, updated, oversized := wd.applyUpdates(ctx, updates)
	report.WikiPagesCreated = created
	report.WikiPagesUpdated = updated
	if len(oversized) > 0 {
		phaseErrors = append(phaseErrors, fmt.Sprintf("oversized pages: %s", strings.Join(oversized, ", ")))
	}

	// Phase 3b: prospective memory — extract unfulfilled commitments from the
	// same input and hand them to the wired sink (the to-do store). Best-effort:
	// a failed extraction never costs the consolidation cycle.
	if wd.openLoopSink != nil {
		loops, lerr := wd.extractOpenLoops(ctx, synthInput)
		switch {
		case lerr != nil:
			phaseErrors = append(phaseErrors, fmt.Sprintf("open-loops: %v", lerr))
		case len(loops) > 0:
			if added, serr := wd.openLoopSink(ctx, loops); serr != nil {
				phaseErrors = append(phaseErrors, fmt.Sprintf("open-loops-sink: %v", serr))
			} else if added > 0 {
				wd.logger.Info("wiki-dream: open loops captured", "extracted", len(loops), "new", added)
			}
		}
	}

	// Phase 3c: mention-driven 인물 seeding — contacts repeatedly mentioned in
	// this cycle's input get stub pages from the address book (see
	// person_seed.go); later cycles enrich them like any page.
	if n := wd.seedPersonPages(ctx, synthInput); n > 0 {
		created += n
		report.WikiPagesCreated = created
	}

	// Phase 3d: project digests — roll up per-project latest progress from the
	// same input and write each into its project 대표페이지's "## 현재 상태" section
	// (the native "프로젝트 진행상황" 모아보기 screen reads those sections). Best-effort:
	// a failed digest pass never costs the consolidation cycle.
	if digests, derr := wd.extractProjectDigests(ctx, synthInput); derr != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("project-digests: %v", derr))
	} else if len(digests) > 0 {
		if written := wd.applyProjectDigests(digests, time.Now()); written > 0 {
			wd.logger.Info("wiki-dream: project status updated", "written", written)
		}
	}

	// Phase 3e: procedural memory — promote the active 사용자 (user-preference)
	// pages into USER.md's managed "행동 지침" section so standing directives are
	// *applied* every turn (prompt context file) instead of only recalled. The
	// dreamer's existing 사용자 synthesis is the distiller; supersede is the
	// consolidation. Opt-in (DENEB_USER_DIRECTIVES) and best-effort: a failed
	// pass never costs the consolidation cycle (see user_directives.go).
	if userDirectivesEnabled() {
		if n, derr := wd.distillUserDirectives(); derr != nil {
			phaseErrors = append(phaseErrors, fmt.Sprintf("user-directives: %v", derr))
		} else if n > 0 {
			wd.logger.Info("wiki-dream: user directives applied", "directives", n)
		}
	}

	// Phase 4: Rebuild index.
	if err := wd.rebuildIndex(); err != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("index-rebuild: %v", err))
	}

	// Phase 5: Verify existing pages (duplicate detection + misclassification),
	// then AUTO-APPLY the high-confidence fixes (exact-duplicate merge, LLM
	// high-confidence category move). Low-confidence findings stay advisory in
	// the report; auto-applied corrections are logged and capped per cycle, and
	// are reversible from this cycle's git snapshot.
	findings := wd.verifyPages(ctx)
	if len(findings) > 0 {
		applied := wd.applyVerifyFixes(findings)
		for _, f := range findings {
			if f.Fix != nil {
				continue // high-confidence: auto-applied (or attempted, logged), not advisory
			}
			report.VerifyFindings = append(report.VerifyFindings, f.Detail)
		}
		wd.logger.Info("wiki-dream: verification", "findings", len(findings), "autoApplied", applied)
		if applied > 0 {
			// The moves/merges changed the page set — rebuild so the snapshot
			// (Phase 6) and next cycle see the corrected wiki.
			if err := wd.rebuildIndex(); err != nil {
				phaseErrors = append(phaseErrors, fmt.Sprintf("index-rebuild after auto-fix: %v", err))
			}
		}
	}

	// Phase 5.5: Densify the graph. For pages that have no related links yet,
	// suggest a couple of semantic neighbors (high cosine floor) and wire them.
	// Additive only — never removes a link — and a no-op without an embedder.
	// Runs before the snapshot so the new edges land in this cycle's graph.
	if enriched := wd.enrichRelatedLinks(ctx); enriched > 0 {
		wd.logger.Info("wiki-dream: related-link enrichment", "linksAdded", enriched)
	}

	// Phase 6: Project the wiki into a graphify-compatible graph.json so the
	// `graphify` tool can query, traverse, and cluster wiki concepts. No LLM
	// call here — synthesize() already curates Related[], we just serialize.
	if outDir, ok := graphSnapshotOutDir(); ok {
		snap, snapErr := BuildGraphSnapshot(ctx, wd.store, outDir, true)
		if snapErr != nil {
			phaseErrors = append(phaseErrors, fmt.Sprintf("graph-snapshot: %v", snapErr))
		} else {
			report.WikiGraphNodes = snap.Nodes
			report.WikiGraphEdges = snap.Edges
			report.WikiGraphClustered = snap.Clustered
			if snap.ClusterError != "" {
				wd.logger.Warn("wiki-dream: graph cluster step failed",
					"error", snap.ClusterError)
			}
			wd.logger.Info("wiki-dream: graph snapshot",
				"nodes", snap.Nodes, "edges", snap.Edges,
				"clustered", snap.Clustered, "out", snap.GraphPath)
		}
	}

	// Phase 4b: curate MEMORY.md now that its consumed sections are distilled
	// into wiki pages, and advance the high-water mark for the state save below.
	if memScan != nil {
		if _, derr := wd.curateWorkspaceMemory(memScan); derr != nil {
			phaseErrors = append(phaseErrors, fmt.Sprintf("memory-curation: %v", derr))
		}
		scan.State.MemoryConsumedThrough = memScan.ConsumedThrough
	}

	// Persist diary high-water state only after synthesis/apply/index work has
	// completed. LastProcessed remains for display and legacy migration, but
	// scanDiaries uses per-file offsets as the primary source of truth.
	idx := wd.store.Index()
	if scan != nil && scan.LatestDate != "" {
		idx.LastProcessed = scan.LatestDate
	} else {
		idx.LastProcessed = time.Now().Format("2006-01-02")
	}
	indexPath := filepath.Join(wd.store.Dir(), "index.md")
	if err := idx.Save(indexPath); err != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("index-save: %v", err))
	}
	if scan != nil {
		scan.State.Recent = appendProcessedDiaryCapsule(scan.State.Recent, processedDiaryCapsule{
			At:        time.Now().Format(time.RFC3339),
			DiaryDate: scan.LatestDate,
			Proposed:  len(updates),
			Created:   created,
			Updated:   updated,
			Paths:     updatePaths(updates),
		})
		if err := wd.saveDiaryProcessState(scan.State); err != nil {
			phaseErrors = append(phaseErrors, fmt.Sprintf("diary-state-save: %v", err))
		}
	}

	wd.resetCounters()
	report.PhaseErrors = phaseErrors
	report.DurationMs = time.Since(start).Milliseconds()
	proposal.Applied = dreamApplySummary{Created: created, Updated: updated}
	proposal.PhaseErrors = phaseErrors
	proposal.DurationMs = report.DurationMs
	if err := wd.saveDreamProposalReport(proposal); err != nil {
		report.PhaseErrors = append(report.PhaseErrors, fmt.Sprintf("proposal-save-final: %v", err))
	}

	// Version the cycle's wiki mutations and surface exactly what changed —
	// pages, snapshot hash, diffstat — plus a one-step rollback hint. A bad
	// LLM cycle becomes a visible, revertible event instead of silent drift.
	if hash := wd.store.SnapshotGit(ctx, fmt.Sprintf("dream: +%d페이지 생성, %d페이지 수정", created, updated)); hash != "" {
		report.WikiChangeSummary = formatWikiChangeSummary(
			hash, wd.store.GitSnapshotStat(ctx, hash), wd.store.Dir(), updatePaths(updates))
	}

	wd.logger.Info("wiki-dream: cycle complete",
		"created", created, "updated", updated,
		"duration", time.Since(start).Round(time.Millisecond))

	return report, nil
}

// scanDiaries reads diary bytes that have not yet been consolidated. The
// primary cursor is a per-file byte offset; index.LastProcessed is only a
// legacy migration hint for old diaries that predate the cursor file.
