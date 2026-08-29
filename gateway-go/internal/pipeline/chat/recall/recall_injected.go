// recall_injected.go — per-session handoff of the wiki paths the preflight
// just injected, consumed once by the end-of-turn citation pass.
//
// The preflight knows WHICH pages entered the turn's context; only the end of
// the turn knows whether the final answer actually referenced any of them.
// This in-memory registry is the line between the two: Build stores the
// turn's injected paths, finishTurnSideEffects consumes them via
// RecordAnswerCitations and appends cite events for the ones the answer
// referenced. In-memory and best-effort by design — a restart mid-turn loses
// one turn's citation telemetry, nothing more (same discipline as the
// snapshot cache in recall_cache.go; single-operator deployment).
package recall

import (
	"log/slog"
	"path"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

var recallInjectedStore = struct {
	mu    sync.Mutex
	store map[string][]string // session key → last preflight's injected wiki paths
}{store: make(map[string][]string)}

// StoreInjectedPaths replaces the pending citation candidates for the
// session. Empty paths clears the slot — a turn that injected nothing must
// not let a previous turn's paths be attributed to its answer.
func StoreInjectedPaths(sessionKey string, paths []string) {
	if sessionKey == "" {
		return
	}
	recallInjectedStore.mu.Lock()
	defer recallInjectedStore.mu.Unlock()
	if len(paths) == 0 {
		delete(recallInjectedStore.store, sessionKey)
		return
	}
	recallInjectedStore.store[sessionKey] = append([]string(nil), paths...)
}

// snapshotWikiRefPattern extracts the wiki/org page refs from a RENDERED
// recall block — the inverse of formatRecallEvidenceAt's row shape, kept in
// this package so renderer and parser move together (round-trip pinned by
// TestArmSnapshotCitationsRoundTrip).
var snapshotWikiRefPattern = regexp.MustCompile(`(?m)^- source=(wiki|org) ref="([^"]+)"`)

// ArmSnapshotCitations re-arms the end-of-turn citation pass for a turn served
// from the frozen snapshot cache. A cache hit skips Build entirely, so without
// this the SECOND and later turns of a pinned-topic conversation — exactly the
// turns most likely to keep using the pinned evidence — could never earn cite
// events (the candidates are consume-once). Inject events are NOT re-recorded:
// exposure was already counted when the snapshot was built; only the use half
// needs re-arming. Org rows count only when they point at a real page, the
// same rule isLedgerPage applies on the evidence structs.
func ArmSnapshotCitations(sessionKey, snapshot string) {
	if sessionKey == "" || snapshot == "" {
		return
	}
	matches := snapshotWikiRefPattern.FindAllStringSubmatch(snapshot, -1)
	if len(matches) == 0 {
		return
	}
	paths := make([]string, 0, len(matches))
	for _, m := range matches {
		if m[1] == "org" && !strings.HasSuffix(m[2], ".md") {
			continue
		}
		paths = append(paths, m[2])
	}
	if len(paths) > 0 {
		StoreInjectedPaths(sessionKey, paths)
	}
}

// TakeInjectedPaths returns and clears the session's pending candidates —
// consume-once, so one answer gets exactly one citation pass.
func TakeInjectedPaths(sessionKey string) []string {
	if sessionKey == "" {
		return nil
	}
	recallInjectedStore.mu.Lock()
	defer recallInjectedStore.mu.Unlock()
	paths := recallInjectedStore.store[sessionKey]
	delete(recallInjectedStore.store, sessionKey)
	return paths
}

// RecordAnswerCitations closes the loop the preflight opened: of the wiki
// pages injected as recall evidence this turn, it records a cite event for
// each one the final answer plausibly referenced. Injection alone is
// exposure, not use (bridge-evidence adoption) — this is the use half.
// Always consumes the pending candidates, even when it cannot score them, so
// stale paths never leak into a later turn. Best-effort: a ledger failure is
// Warn-logged and never affects delivery.
func RecordAnswerCitations(store *wiki.Store, sessionKey, answer string, logger *slog.Logger) {
	injected := TakeInjectedPaths(sessionKey)
	if store == nil || len(injected) == 0 {
		return
	}
	cited := matchCitedPaths(answer, injected, pageIdentityResolver(store))
	// Content-grounded second pass (recall_cite_content.go): pages the answer
	// used without ever naming — it quotes their figures and proper nouns
	// instead. Only paths the name pass missed are re-examined, bounded by the
	// handful of injected pages per turn, all after delivery.
	cited = appendContentCitations(cited, injected, answer, store)
	if len(cited) == 0 {
		return
	}
	events := make([]wiki.RecallEvent, 0, len(cited))
	for _, p := range cited {
		events = append(events, wiki.RecallEvent{Path: p, Event: wiki.RecallEventCite, Session: sessionKey})
	}
	if err := store.RecordRecallEvents(events); err != nil && logger != nil {
		logger.Warn("recall cite: ledger write failed", "session", sessionKey, "error", err)
	}
}

// citeGenericTitles are page titles too generic to trust as a bare-title
// citation match — an answer saying "현황을 정리하면" must not credit a page
// that merely happens to be named 현황.md.
var citeGenericTitles = map[string]struct{}{
	"개요": {}, "현황": {}, "정리": {}, "회의": {}, "메모": {},
	"노트": {}, "기록": {}, "목록": {}, "일지": {}, "대표": {},
}

// citeTitleMinRunes is the shortest page title trusted as a bare-title match;
// below it, common-word collisions dominate real citations.
const citeTitleMinRunes = 3

// matchCitedPaths returns the injected paths the answer text references: the
// full rel path (with or without .md), or the page title (base name sans .md)
// when the title is specific enough. Under-counting by design — cite is
// best-effort telemetry, and a false positive would grant unearned utility
// credit to a page the answer never engaged.
func matchCitedPaths(answer string, paths []string, identity func(string) (title, code string)) []string {
	answer = strings.TrimSpace(answer)
	if answer == "" || len(paths) == 0 {
		return nil
	}
	var out []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		trimmed := strings.TrimSuffix(p, ".md")
		if strings.Contains(answer, trimmed) { // also covers the full path-with-.md form
			out = append(out, p)
			continue
		}
		if citeNameMatches(answer, path.Base(trimmed)) {
			out = append(out, p)
			continue
		}
		// Filename matching cannot see mail analyses: their filename is the Gmail
		// message id (61% of injected client paths in the 30-day ledger are
		// uuid/hash shaped), so an answer about that mail never contains anything
		// path-like. The page's own title — the mail subject, the project's
		// name — is what an answer quotes, and the frozen project code is a
		// distinctive token answers use verbatim.
		if identity == nil {
			continue
		}
		title, code := identity(p)
		if citeNameMatches(answer, title) || (code != "" && strings.Contains(answer, code)) {
			out = append(out, p)
		}
	}
	return out
}

// citeNameMatches applies the same conservatism to any name-shaped token: long
// enough to be distinctive, not one of the words every page carries.
func citeNameMatches(answer, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) < citeTitleMinRunes {
		return false
	}
	if _, generic := citeGenericTitles[name]; generic {
		return false
	}
	return strings.Contains(answer, name)
}

// appendContentCitations runs the content-grounded matcher over the injected
// paths the name-based pass did not credit, appending the ones whose
// distinctive body content the answer reproduces.
func appendContentCitations(cited, injected []string, answer string, store *wiki.Store) []string {
	if store == nil {
		return cited
	}
	already := make(map[string]struct{}, len(cited))
	for _, p := range cited {
		already[p] = struct{}{}
	}
	for _, p := range injected {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, done := already[p]; done {
			continue
		}
		page, err := store.ReadPage(p)
		if err != nil || page == nil {
			continue
		}
		if citeContentMatches(answer, page.Body, store.TokenRarity) {
			cited = append(cited, p)
		}
	}
	return cited
}

// pageIdentityResolver reads the title and project code of an injected page.
// Bounded by construction: recall injects at most a handful of pages per turn,
// and the reads happen after delivery.
func pageIdentityResolver(store *wiki.Store) func(string) (string, string) {
	if store == nil {
		return nil
	}
	return func(relPath string) (string, string) {
		page, err := store.ReadPage(relPath)
		if err != nil || page == nil {
			return "", ""
		}
		return strings.TrimSpace(page.Meta.Title), strings.TrimSpace(page.Meta.Code)
	}
}
