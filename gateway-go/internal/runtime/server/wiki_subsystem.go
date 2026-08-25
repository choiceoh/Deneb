// wiki_subsystem.go — Enabled-wiki construction: store, fact-plane cutover,
// change mirroring, query expansion, and the dreamer.
//
// Extracted from initMemorySubsystem so the cutover can decide between
// restarting and degrading with an early return instead of nesting the whole
// wiring block behind it.
package server

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
)

// initWikiSubsystem builds the wiki store and runs the one-time fact-plane
// cutover, then wires everything that depends on it.
//
// A returned error stops startup. A nil return with wiki left unwired means the
// cutover has failed deterministically (see fact_cutover_guard.go): the gateway
// serves without the fact plane rather than restarting into the same failure
// forever.
func (s *Server) initWikiSubsystem(chatCfg *chat.HandlerConfig, reg *modelrole.Registry, wikiCfg wiki.Config) error {
	wikiMemoryInitMu.Lock()
	defer wikiMemoryInitMu.Unlock()
	if s.factCutover == nil {
		s.factCutover = newFactCutoverGuard(s.logger)
	}
	wikiStore, err := wiki.NewStore(wikiCfg.Dir, wikiCfg.DiaryDir)
	if err != nil {
		// A store that cannot open loops exactly like a failing cutover does,
		// so it is counted on the same budget.
		if s.degradeWikiAfterRepeatedFailure(err, wikiCfg.Dir) {
			return nil
		}
		return fmt.Errorf("initialize enabled wiki store: %w", err)
	}
	closeOnCutoverFailure := func(cutoverErr error) error {
		if closeErr := wikiStore.Close(); closeErr != nil {
			s.logger.Warn("wiki close after fact-plane cutover failure", "error", closeErr)
		}
		return cutoverErr
	}
	// One-time fact-plane cutover: import fact-sized legacy context
	// before MEMORY.md/USER.md become generated compatibility views.
	// Import is source+value idempotent, so a restart after a partial
	// migration resumes without duplicating committed facts.
	workspaceDir := configresolve.WorkspaceDir()
	var cutoverErr error
	if strings.TrimSpace(workspaceDir) == "" {
		cutoverErr = errors.New("workspace dir is empty")
	}
	imported := 0
	if cutoverErr == nil {
		imported, cutoverErr = wikiStore.ImportLegacyFactFiles(workspaceDir)
		if cutoverErr != nil {
			cutoverErr = fmt.Errorf("import legacy facts: %w", cutoverErr)
		}
		// Bullets the cutover could not convert are skipped, not fatal — but
		// never silently: they stay in the preserved *.legacy.md copy and this
		// is the only place anyone would learn they did not become facts.
		if skips := wikiStore.LegacyFactImportSkips(); len(skips) > 0 {
			s.logger.Warn("wiki: legacy fact bullets skipped during cutover",
				"count", len(skips), "examples", strings.Join(skips, "; "))
		}
	}
	if cutoverErr == nil {
		cutoverErr = wikiStore.SetFactProjectionDir(workspaceDir)
		if cutoverErr != nil {
			cutoverErr = fmt.Errorf("configure fact projections: %w", cutoverErr)
		}
	}
	if cutoverErr != nil {
		wrapped := fmt.Errorf("initialize enabled wiki fact plane for workspace %q: %w", workspaceDir, cutoverErr)
		// Never serve a hybrid state where the journal advanced but frozen
		// MEMORY/USER still carry the retired value. Keep the legacy context
		// files untouched and disable wiki for this process; the idempotent
		// migration resumes on the next clean startup.
		//
		// That restart only helps if the failure can clear. Past
		// maxFactCutoverAttempts consecutive startups it demonstrably cannot,
		// and continuing to exit turns a degraded fact plane into an
		// unavailable gateway (2026-08-23: 150 restarts, 28 minutes down).
		if s.degradeWikiAfterRepeatedFailure(cutoverErr, workspaceDir) {
			if closeErr := wikiStore.Close(); closeErr != nil {
				s.logger.Warn("wiki close after fact-plane cutover failure", "error", closeErr)
			}
			return nil
		}
		return closeOnCutoverFailure(wrapped)
	}
	s.factCutover.recordSuccess()
	// One-time identity repair for legacy sentence-keyed facts (fact_rekey.go).
	// Runs after the cutover and BEFORE the fact-derived revision is approved,
	// so prompt snapshots bind to the post-migration revision. Idempotent —
	// already-migrated keys have no active claim and are skipped.
	if moved, rerr := wikiStore.RekeyLegacyFacts(); rerr != nil {
		// Non-fatal: a half-done rekey leaves both identities readable and the
		// next startup resumes where it stopped. Crashing here would trade a
		// cosmetic identity for availability (the 2026-08-23 lesson).
		s.logger.Warn("wiki: legacy fact rekey incomplete", "moved", moved, "error", rerr)
	} else if moved > 0 {
		s.logger.Info("wiki: legacy facts re-keyed to axis identities", "moved", moved)
	}
	// Bind persisted prompt snapshots to the canonical journal revision
	// before asynchronous session restore. This closes both first-cutover and
	// commit-before-cache-clear crash windows: an older Tier1/MEMORY snapshot
	// is sanitized even when an idempotent restart imports zero new rows.
	chat.SetFactDerivedRevision(uint64(wikiStore.LatestFactRevision()))

	s.wikiStore = wikiStore
	wikiStore.SetFactJournalFailureObserver(s.handleFactJournalFailure)
	chatCfg.Memory.Wiki = wikiStore
	s.logger.Info("wiki knowledge base and fact plane enabled", "dir", wikiCfg.Dir,
		"revision", wikiStore.LatestFactRevision(), "legacyImported", imported)

	// Mirror meaningful wiki writes/deletes onto the native-sync stream so
	// clients drop their page/category snapshots promptly instead of waiting
	// out their TTL (the calendar observer's pattern). The store is the
	// single choke point every writer funnels through: agent tools, miniapp
	// RPCs, the dreamer. Append failure is Warn, not Error — clients heal
	// via TTL revalidation, so no user-observable loss.
	if s.nativeSyncStore != nil {
		wikiStore.SetChangeObserver(s.ShutdownCtx(), func(relPath string) {
			if _, err := s.nativeSyncStore.Append(nativesync.WikiChanged(relPath)); err != nil {
				s.logger.Warn("native sync: wiki change append failed", "path", relPath, "error", err)
			}
		})
	}

	// Vocabulary-gap query expander (tiny role). Dormant unless
	// DENEB_WIKI_QUERY_EXPANSION=backfill — the store only invokes it
	// when that gate is on AND a query under-fills its result limit
	// (domain/wiki/query_expansion.go), so this wiring is free at rest.
	if tinyClient, tinyModel := reg.Client(modelrole.RoleTiny), reg.Model(modelrole.RoleTiny); tinyClient != nil && tinyModel != "" {
		// Registry-aware thinking-off (dreamerLLMShape's three-way,
		// scoped to the tiny role): a dual-mode reasoning tiny would
		// otherwise spend the whole expansion budget on thinking.
		var extraBody map[string]any
		tinyCfg := reg.Config(modelrole.RoleTiny)
		if directive := reg.ThinkingOffDirectiveFor(tinyCfg.ProviderID, tinyCfg.Model); directive != nil {
			extraBody = map[string]any{
				"chat_template_kwargs": map[string]any{directive.TemplateKwarg(): false},
			}
		}
		wikiStore.SetQueryExpander(makeWikiQueryExpander(tinyClient, tinyModel, extraBody, s.logger))
	}

	// Wiki dreamer. This bounded JSON-synthesis lane favors the tiny
	// model: on srv4 that is local dsv4-nothink, avoiding a dependency on
	// the cloud lightweight provider for autonomous memory maintenance.
	dreamClient := reg.Client(wikiDreamerModelRole)
	dreamModel := reg.Model(wikiDreamerModelRole)
	if dreamClient != nil && dreamModel != "" {
		s.wikiDreamer = wiki.NewWikiDreamer(wikiStore, dreamClient, dreamModel, wikiCfg, s.logger)
		// Shape the dreamer's raw LLM calls for the selected tiny model:
		// thinking off on dual-mode reasoning models (deepseek-v4's
		// chain-of-thought consumed the whole 4096-token synthesis
		// budget — 2026-07-02/03 "empty content (finish_reason=length)"
		// dream failures), reasoning headroom when no off-switch exists.
		extra, synthMax := dreamerLLMShape(reg)
		s.wikiDreamer.SetLLMRequestShape(extra, synthMax)
		// Let dream cycles consume + curate the auto-recorded
		// workspace MEMORY.md (distill to wiki, keep a bounded buffer).
		s.wikiDreamer.SetWorkspaceDir(configresolve.WorkspaceDir())
		// RHI self-comparison + synthesis-rules revision (arXiv
		// 2607.15524): production only — the revised
		// wiki-dream-rules.md lives in the shared workspace a
		// dev/live-test gateway must not mutate. Fail-closed.
		if home, err := os.UserHomeDir(); err == nil {
			if _, ok := s.productionStateDir(home); ok && os.Getenv("DENEB_DREAM_RULES_EVOLVE") != "0" {
				s.wikiDreamer.SetRulesEvolution(true)
			}
		}
		// Open loops are no longer auto-recorded as to-dos (operator approval
		// first) — no open-loop sink is wired (the dreamer skips it when nil).
		// Per-project latest-progress digests are written directly into each
		// project 대표페이지's "## 현재 상태" section by the dream cycle itself
		// (no sink — the dreamer owns the wiki store; see project_digest.go),
		// and kept fresh between cycles by the mail-analysis sink.
		// Mention-driven 인물 seeding from the contacts mirror.
		if cs := s.contactsStore; cs != nil {
			s.wikiDreamer.SetPersonDirectory(func() []wiki.PersonSeed {
				all := cs.All()
				seeds := make([]wiki.PersonSeed, 0, len(all))
				for _, c := range all {
					seeds = append(seeds, wiki.PersonSeed{
						Name: c.Name, Org: c.Org, Phones: c.Phones, Emails: c.Emails,
					})
				}
				return seeds
			})
		}
		s.logger.Info("wiki-dream: enabled")
	}
	return nil
}

// degradeWikiAfterRepeatedFailure counts one failed wiki initialization and
// reports whether this process should now start with wiki disabled instead of
// exiting into another identical restart.
//
// False keeps the caller on the historical fail-closed path. True means the
// caller must leave wiki unwired and return nil; the fact-derived revision is
// pinned to the legacy epoch here so the caller cannot forget it.
func (s *Server) degradeWikiAfterRepeatedFailure(cause error, dir string) bool {
	attempts := s.factCutover.recordFailure(cause)
	if attempts < maxFactCutoverAttempts {
		return false
	}
	// Revision zero is the legacy/no-fact-plane epoch, the same value a
	// configured-off wiki approves: without it every restart sanitizes and then
	// refuses to persist Tier1/context snapshots.
	chat.SetFactDerivedRevision(0)
	s.logger.Error("wiki initialization keeps failing; starting with wiki DISABLED to stop the restart loop",
		"attempts", attempts, "dir", dir, "error", cause,
		"action", "fix the cause and restart; the counter clears on the next successful startup")
	return true
}
