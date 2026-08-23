// fact_read.go — read paths over the committed fact plane: revision cursors,
// per-subject active/history views, and the single-revision recall snapshot that
// pairs current winners with the retired values recall must suppress. Mutation
// and durability live in fact_store.go; segmentation in fact_journal.go.

package wiki

import (
	"log/slog"
	"sort"
	"strings"
)

// LatestFactRevision returns the current durable journal cursor.
func (s *Store) LatestFactRevision() FactRevision {
	if s == nil {
		return 0
	}
	s.factMu.RLock()
	defer s.factMu.RUnlock()
	return s.factState.Revision
}

// FactProjectionStatus reports whether rebuildable fact compatibility views
// are caught up with the canonical journal revision. A degraded projection does
// not make Facts/Search unavailable and must not cause callers to retry a
// mutation whose FactWriteResult.Committed field is already true.
type FactProjectionStatus struct {
	Revision        FactRevision `json:"revision"`
	Degraded        bool         `json:"degraded"`
	ProjectionError string       `json:"projectionError,omitempty"`
	JournalError    string       `json:"journalError,omitempty"`
}

func (s *Store) FactProjectionStatus() FactProjectionStatus {
	if s == nil {
		return FactProjectionStatus{}
	}
	s.factMu.RLock()
	defer s.factMu.RUnlock()
	return FactProjectionStatus{
		Revision:        s.factState.Revision,
		Degraded:        s.factProjectionError != "" || s.factJournalPoisoned != "",
		ProjectionError: s.factProjectionError,
		JournalError:    s.factJournalPoisoned,
	}
}

// SetFactJournalFailureObserver registers the process-fatal boundary for an
// ambiguous journal append. The callback must not call back into Store: it runs
// while the fact mutation lock is held so no reader can observe a half-decided
// revision. The server uses it only to close admission and start shutdown.
func (s *Store) SetFactJournalFailureObserver(observer func(error)) {
	if s == nil {
		return
	}
	s.factMu.Lock()
	s.factJournalFailureObserver = observer
	s.factMu.Unlock()
}

// SnapshotFacts returns a deep copy safe for callers to retain and inspect.
func (s *Store) SnapshotFacts() FactSnapshot {
	if s == nil {
		return FactSnapshot{SchemaVersion: factSchemaVersion, Facts: map[string][]FactClaim{}}
	}
	s.factMu.RLock()
	defer s.factMu.RUnlock()
	return cloneFactSnapshot(s.factState)
}

// ActiveFacts returns current and unresolved-conflict claims for subject. An
// empty subject returns every subject; callers serving personal context should
// normally pass "self" to preserve cross-subject isolation.
func (s *Store) ActiveFacts(subject string) []FactClaim {
	_, claims := s.ActiveFactSnapshot(subject)
	return claims
}

// ActiveFactSnapshot returns the durable revision and its active claims from
// one factMu read-side critical section. Callers that label or render claims
// with a revision must use this instead of pairing ActiveFacts with a later
// LatestFactRevision call, which could observe a correction between the reads.
func (s *Store) ActiveFactSnapshot(subject string) (FactRevision, []FactClaim) {
	if s == nil {
		return 0, nil
	}
	subject = strings.TrimSpace(subject)
	if subject != "" {
		subject = normalizeFactSubject(subject)
	}
	s.factMu.RLock()
	defer s.factMu.RUnlock()
	revision := s.factState.Revision
	var out []FactClaim
	for _, identity := range sortedFactIdentities(s.factState.Facts) {
		for _, claim := range s.factState.Facts[identity] {
			if subject != "" && claim.Subject != subject {
				continue
			}
			if claim.Status != FactStatusCurrent && claim.Status != FactStatusConflicted {
				continue
			}
			out = append(out, cloneFactClaim(claim))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RecordedAtMs != out[j].RecordedAtMs {
			return out[i].RecordedAtMs > out[j].RecordedAtMs
		}
		if out[i].Subject != out[j].Subject {
			return out[i].Subject < out[j].Subject
		}
		return out[i].Key < out[j].Key
	})
	return revision, out
}

// RecallFactSnapshot returns all lifecycle inputs used by retrieval from one
// canonical revision. The snapshot is immutable from the caller's perspective.
func (s *Store) RecallFactSnapshot() FactRecallSnapshot {
	if s == nil {
		return FactRecallSnapshot{}
	}
	s.factMu.RLock()
	defer s.factMu.RUnlock()

	out := FactRecallSnapshot{Revision: s.factState.Revision}
	staleSeen := make(map[string]struct{})
	correctedSeen := make(map[string]struct{})
	for _, identity := range sortedFactIdentities(s.factState.Facts) {
		claims := s.factState.Facts[identity]
		rule := FactLifecycleRule{}
		currentSeen := make(map[string]struct{})
		retiredSeen := make(map[string]struct{})
		hasRetiredValue := false
		hasActive := false
		hasLiveTombstone := false
		for _, claim := range claims {
			if rule.Subject == "" {
				rule.Subject = claim.Subject
				rule.Key = claim.Key
				rule.Kind = claim.Kind
			}
			switch claim.Status {
			case FactStatusCurrent, FactStatusConflicted:
				hasActive = true
				out.Active = append(out.Active, cloneFactClaim(claim))
				appendFactLifecycleValue(&rule.CurrentValues, currentSeen, claim.Value)
			case FactStatusSuperseded, FactStatusTombstoned:
				if strings.TrimSpace(claim.Value) != "" {
					hasRetiredValue = true
					appendFactLifecycleValue(&rule.StaleValues, retiredSeen, claim.Value)
				}
				if claim.Status == FactStatusTombstoned && strings.TrimSpace(claim.Value) == "" {
					hasLiveTombstone = true
				}
			}
		}
		if hasRetiredValue || hasLiveTombstone && !hasActive {
			rule.Tombstoned = hasLiveTombstone && !hasActive
			// A value may become canonical again after a tombstone. Current wins
			// within the same identity; retaining that value in the stale side
			// would make the rebuilt snapshot reject its own live evidence.
			rule.StaleValues = removeCurrentFactLifecycleValues(rule.StaleValues, currentSeen)
			sortFactLifecycleValues(rule.CurrentValues)
			sortFactLifecycleValues(rule.StaleValues)
			out.LifecycleRules = append(out.LifecycleRules, rule)
			correctedSeen[normalizeFactKey(rule.Key)] = struct{}{}
		}
	}
	for folded, value := range s.supersededPageStale {
		if _, seen := staleSeen[folded]; seen {
			continue
		}
		staleSeen[folded] = struct{}{}
		out.StaleValues = append(out.StaleValues, value)
	}
	for key := range correctedSeen {
		out.CorrectedKeys = append(out.CorrectedKeys, key)
	}
	sort.SliceStable(out.Active, func(i, j int) bool {
		if out.Active[i].RecordedAtMs != out.Active[j].RecordedAtMs {
			return out.Active[i].RecordedAtMs > out.Active[j].RecordedAtMs
		}
		if out.Active[i].Subject != out.Active[j].Subject {
			return out.Active[i].Subject < out.Active[j].Subject
		}
		return out.Active[i].Key < out.Active[j].Key
	})
	sort.SliceStable(out.StaleValues, func(i, j int) bool {
		leftRunes, rightRunes := len([]rune(out.StaleValues[i])), len([]rune(out.StaleValues[j]))
		if leftRunes != rightRunes {
			return leftRunes > rightRunes
		}
		return out.StaleValues[i] < out.StaleValues[j]
	})
	sort.SliceStable(out.LifecycleRules, func(i, j int) bool {
		if out.LifecycleRules[i].Subject != out.LifecycleRules[j].Subject {
			return out.LifecycleRules[i].Subject < out.LifecycleRules[j].Subject
		}
		return out.LifecycleRules[i].Key < out.LifecycleRules[j].Key
	})
	sort.Strings(out.CorrectedKeys)
	return out
}

func appendFactLifecycleValue(values *[]string, seen map[string]struct{}, raw string) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return
	}
	folded := strings.ToLower(strings.Join(strings.Fields(value), " "))
	if _, exists := seen[folded]; exists {
		return
	}
	seen[folded] = struct{}{}
	*values = append(*values, value)
}

func sortFactLifecycleValues(values []string) {
	sort.SliceStable(values, func(i, j int) bool {
		leftRunes, rightRunes := len([]rune(values[i])), len([]rune(values[j]))
		if leftRunes != rightRunes {
			return leftRunes > rightRunes
		}
		return values[i] < values[j]
	})
}

func removeCurrentFactLifecycleValues(values []string, current map[string]struct{}) []string {
	out := values[:0]
	for _, value := range values {
		folded := strings.ToLower(strings.Join(strings.Fields(value), " "))
		if _, isCurrent := current[folded]; !isCurrent {
			out = append(out, value)
		}
	}
	return out
}

// FactHistory returns every retained version of one identity in revision order.
func (s *Store) FactHistory(subject, key string) []FactClaim {
	if s == nil {
		return nil
	}
	identity := factIdentity(subject, key)
	s.factMu.RLock()
	defer s.factMu.RUnlock()
	claims := s.factState.Facts[identity]
	out := make([]FactClaim, len(claims))
	for i := range claims {
		out[i] = cloneFactClaim(claims[i])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	return out
}

// ActiveFactByID resolves a synthetic fact-search reference only while the
// immutable claim is still current or conflicted. A ref returned before a
// correction must fail on a later read rather than reopening superseded data.
func (s *Store) ActiveFactByID(id string) (FactClaim, bool) {
	if s == nil || strings.TrimSpace(id) == "" {
		return FactClaim{}, false
	}
	id = strings.TrimSpace(id)
	s.factMu.RLock()
	defer s.factMu.RUnlock()
	for _, claims := range s.factState.Facts {
		for _, claim := range claims {
			if claim.ID != id || claim.Status != FactStatusCurrent && claim.Status != FactStatusConflicted {
				continue
			}
			return cloneFactClaim(claim), true
		}
	}
	return FactClaim{}, false
}

// StaleFactValues is the compatibility/diagnostic view of values explicitly
// superseded or forgotten. Retrieval must use RecallFactSnapshot so typed
// values retain subject/key identity instead of becoming a global deny set.
func (s *Store) StaleFactValues(subject string) []string {
	if s == nil {
		return nil
	}
	subject = strings.TrimSpace(subject)
	if subject != "" {
		subject = normalizeFactSubject(subject)
	}
	s.factMu.RLock()
	defer s.factMu.RUnlock()
	seen := map[string]struct{}{}
	var out []string
	for _, claims := range s.factState.Facts {
		for _, claim := range claims {
			if subject != "" && claim.Subject != subject {
				continue
			}
			if claim.Status != FactStatusSuperseded && claim.Status != FactStatusTombstoned {
				continue
			}
			value := strings.TrimSpace(claim.Value)
			if value == "" {
				continue
			}
			folded := strings.ToLower(strings.Join(strings.Fields(value), " "))
			if _, ok := seen[folded]; ok {
				continue
			}
			seen[folded] = struct{}{}
			out = append(out, value)
		}
	}
	// Superseded wiki pages are untyped, so expose their stale lines only on
	// the all-subject read used by recall. A subject-scoped caller must not
	// accidentally inherit unrelated project/page values.
	if subject == "" {
		for folded, value := range s.supersededPageStale {
			if _, ok := seen[folded]; ok {
				continue
			}
			seen[folded] = struct{}{}
			out = append(out, value)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		leftRunes, rightRunes := len([]rune(out[i])), len([]rune(out[j]))
		if leftRunes != rightRunes {
			return leftRunes > rightRunes
		}
		return out[i] < out[j]
	})
	return out
}

func (s *Store) loadSupersededPageStaleValues() error {
	pages, err := s.ListPages("")
	if err != nil {
		return err
	}
	stale := make(map[string]string)
	current := make(map[string]struct{})
	for _, relPath := range pages {
		page, err := s.ReadPage(relPath)
		if err != nil || page == nil || !IsEffectivelySuperseded(relPath, page.Meta) {
			continue
		}
		cacheSupersededPageStaleLines(stale, page)

		successorPath := normalizePagePath(page.Meta.SupersededBy)
		successor, successorErr := s.ReadPage(successorPath)
		if successorErr != nil || successor == nil || IsEffectivelySuperseded(successorPath, successor.Meta) {
			continue
		}
		for folded := range normalizedSupersededPageLines(successor) {
			current[folded] = struct{}{}
		}
	}
	// A terminal successor is current evidence. If an older page repeats the
	// same normalized sentence, the current copy wins globally instead of the
	// untyped deny list rejecting its own successor.
	for folded := range current {
		delete(stale, folded)
	}

	s.factMu.Lock()
	s.supersededPageStale = stale
	s.factMu.Unlock()
	return nil
}

func (s *Store) cacheSupersededPageStale(page *Page) {
	if s == nil || page == nil {
		return
	}
	// MarkSuperseded holds writeMu, so rebuilding here observes one coherent
	// page-lifecycle state. Supersession is rare, and a rebuild also handles a
	// newly extended A -> B -> C chain without retaining B as current evidence.
	if err := s.loadSupersededPageStaleValues(); err != nil {
		slog.Warn("wiki: rebuild superseded page stale cache", "error", err)
	}
}

func cacheSupersededPageStaleLines(stale map[string]string, page *Page) {
	if page == nil {
		return
	}
	for _, rawLine := range strings.Split(page.Body, "\n") {
		line, folded, ok := normalizeSupersededPageLine(rawLine)
		if !ok {
			continue
		}
		if _, exists := stale[folded]; !exists {
			stale[folded] = line
		}
	}
}

func normalizedSupersededPageLines(page *Page) map[string]struct{} {
	lines := make(map[string]struct{})
	if page == nil {
		return lines
	}
	for _, rawLine := range strings.Split(page.Body, "\n") {
		_, folded, ok := normalizeSupersededPageLine(rawLine)
		if ok {
			lines[folded] = struct{}{}
		}
	}
	return lines
}

func normalizeSupersededPageLine(rawLine string) (string, string, bool) {
	rawLine = strings.TrimSpace(rawLine)
	if rawLine == "" || strings.HasPrefix(rawLine, "#") || isMarkdownSeparatorLine(rawLine) {
		return "", "", false
	}
	line := strings.TrimSpace(strings.TrimLeft(rawLine, "-*`> "))
	if line == "" || strings.HasPrefix(line, "#") || isMarkdownTableLine(line) ||
		isMarkdownSeparatorLine(line) || isPureWikiLinkLine(line) {
		return "", "", false
	}
	if n := len([]rune(line)); n < 1 || n > 500 {
		return "", "", false
	}
	folded := strings.ToLower(strings.Join(strings.Fields(line), " "))
	if folded == "" {
		return "", "", false
	}
	return line, folded, true
}

func isMarkdownTableLine(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "|") || strings.HasSuffix(line, "|") || strings.Count(line, "|") >= 2
}

func isMarkdownSeparatorLine(line string) bool {
	compact := strings.Join(strings.Fields(line), "")
	if len(compact) < 3 {
		return false
	}
	marker := compact[0]
	if marker != '-' && marker != '*' && marker != '_' {
		return false
	}
	for index := 1; index < len(compact); index++ {
		if compact[index] != marker {
			return false
		}
	}
	return true
}

func isPureWikiLinkLine(line string) bool {
	match := wikiLinkRe.FindStringIndex(strings.TrimSpace(line))
	return len(match) == 2 && match[0] == 0 && match[1] == len(strings.TrimSpace(line))
}

// CorrectedFactKeys is the compatibility/diagnostic view of keys that have
// superseded or tombstoned history. Retrieval uses identity-scoped lifecycle
// rules from RecallFactSnapshot.
func (s *Store) CorrectedFactKeys(subject string) []string {
	if s == nil {
		return nil
	}
	subject = strings.TrimSpace(subject)
	if subject != "" {
		subject = normalizeFactSubject(subject)
	}
	s.factMu.RLock()
	defer s.factMu.RUnlock()
	seen := make(map[string]struct{})
	for _, claims := range s.factState.Facts {
		for _, claim := range claims {
			if subject != "" && claim.Subject != subject {
				continue
			}
			if claim.Status != FactStatusSuperseded && claim.Status != FactStatusTombstoned {
				continue
			}
			key := normalizeFactKey(claim.Key)
			seen[key] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
