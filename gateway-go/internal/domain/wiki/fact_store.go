package wiki

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	factJournalFile = ".fact-mutations.jsonl"
	factStateFile   = ".fact-state.json"
)

// UpsertFact resolves and durably records one claim, then refreshes the
// generated wiki/workspace projections. The journal append is fsynced before
// any derived projection changes, so a crash can lose a projection but never
// resurrect the previous canonical fact state after restart.
func (s *Store) UpsertFact(input FactInput) (FactWriteResult, error) {
	return s.upsertFact(input, true)
}

// upsertFact can defer derived views for ordered legacy-import batches. Every
// row is still appended and fsynced independently; only the rebuildable
// snapshot/page/workspace projections are coalesced.
func (s *Store) upsertFact(input FactInput, syncDerived bool) (FactWriteResult, error) {
	if s == nil {
		return FactWriteResult{}, fmt.Errorf("wiki: nil store")
	}
	if err := validateFactInputBounds(input); err != nil {
		return FactWriteResult{}, fmt.Errorf("wiki: %w", err)
	}
	input.Value = strings.TrimSpace(input.Value)
	if input.Value == "" {
		return FactWriteResult{}, fmt.Errorf("wiki: fact value is required")
	}
	input.Subject = normalizeFactSubject(input.Subject)
	input.Key = normalizeFactKey(input.Key)
	kind, err := normalizeFactKindInput(input.Kind)
	if err != nil {
		return FactWriteResult{}, fmt.Errorf("wiki: %w", err)
	}
	input.Kind = kind
	authority, err := normalizeFactAuthorityInput(input.Authority)
	if err != nil {
		return FactWriteResult{}, fmt.Errorf("wiki: %w", err)
	}
	input.Authority = authority
	if factRequiresBasisAt(input.Kind, input.Authority) && input.BasisAt.IsZero() {
		return FactWriteResult{}, fmt.Errorf("wiki: basis time is required for primary_document %s facts", input.Kind)
	}
	input.Sources = normalizeFactSources(input.Sources)
	input.Actor = strings.TrimSpace(input.Actor)
	input.Reason = strings.TrimSpace(input.Reason)

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.factMu.Lock()
	defer s.factMu.Unlock()

	identity := factIdentity(input.Subject, input.Key)
	input.Kind, err = factKindForIdentity(input.Kind, s.factState.Facts[identity])
	if err != nil {
		return FactWriteResult{}, fmt.Errorf("wiki: %w", err)
	}
	now := s.factTime(input.At)
	mutation := s.resolveFactMutationLocked(input, now)
	if err := s.validateFactMutationLocked(mutation); err != nil {
		return FactWriteResult{}, fmt.Errorf("wiki: validate fact mutation: %w", err)
	}
	if err := s.appendFactMutationLocked(mutation); err != nil {
		return FactWriteResult{}, err
	}
	if err := s.applyFactMutationLocked(mutation); err != nil {
		return FactWriteResult{}, fmt.Errorf("wiki: apply committed fact mutation %d: %w", mutation.Revision, err)
	}

	result := FactWriteResult{
		Revision: mutation.Revision, Resolution: mutation.Resolution, Committed: true,
	}
	if mutation.Claim != nil {
		result.ClaimID = mutation.Claim.ID
		result.Status = mutation.Claim.Status
	}
	if syncDerived {
		result.ProjectionError = s.syncFactDerivedLocked()
		if result.ProjectionError != "" {
			slog.Error("fact mutation committed with stale projection",
				"revision", mutation.Revision, "error", result.ProjectionError)
		}
	}
	return result, nil
}

// TombstoneFact softly retires one identity. Old values remain in the journal
// and snapshot with tombstoned status, while all active recall/projections omit
// them. The tombstone claim also blocks lower-authority inference from silently
// recreating a forgotten value.
func (s *Store) TombstoneFact(input FactTombstoneInput) (FactWriteResult, error) {
	if s == nil {
		return FactWriteResult{}, fmt.Errorf("wiki: nil store")
	}
	if err := validateFactTombstoneInputBounds(input); err != nil {
		return FactWriteResult{}, fmt.Errorf("wiki: %w", err)
	}
	input.Subject = normalizeFactSubject(input.Subject)
	input.Key = normalizeFactKey(input.Key)
	authority, err := normalizeFactAuthorityInput(input.Authority)
	if err != nil {
		return FactWriteResult{}, fmt.Errorf("wiki: %w", err)
	}
	input.Authority = authority
	input.Sources = normalizeFactSources(input.Sources)
	input.Actor = strings.TrimSpace(input.Actor)
	input.Reason = strings.TrimSpace(input.Reason)

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.factMu.Lock()
	defer s.factMu.Unlock()

	now := s.factTime(input.At)
	mutation := s.resolveFactTombstoneLocked(input, now)
	if err := s.validateFactMutationLocked(mutation); err != nil {
		return FactWriteResult{}, fmt.Errorf("wiki: validate fact tombstone: %w", err)
	}
	if err := s.appendFactMutationLocked(mutation); err != nil {
		return FactWriteResult{}, err
	}
	if err := s.applyFactMutationLocked(mutation); err != nil {
		return FactWriteResult{}, fmt.Errorf("wiki: apply committed fact tombstone %d: %w", mutation.Revision, err)
	}
	result := FactWriteResult{
		Revision: mutation.Revision, Resolution: mutation.Resolution, Committed: true,
		ClaimID: mutation.Claim.ID, Status: mutation.Claim.Status,
	}
	result.ProjectionError = s.syncFactDerivedLocked()
	if result.ProjectionError != "" {
		slog.Error("fact tombstone committed with stale projection",
			"revision", mutation.Revision, "error", result.ProjectionError)
	}
	return result, nil
}

func (s *Store) syncFactDerivedLocked() string {
	var projectionErrors []string
	if err := s.saveFactSnapshotLocked(); err != nil {
		projectionErrors = append(projectionErrors, "snapshot: "+err.Error())
	}
	if err := s.syncFactPageLocked(); err != nil {
		projectionErrors = append(projectionErrors, "wiki: "+err.Error())
	}
	if err := s.syncFactWorkspaceLocked(); err != nil {
		projectionErrors = append(projectionErrors, "workspace: "+err.Error())
	}
	s.factProjectionError = strings.Join(projectionErrors, "; ")
	return s.factProjectionError
}

func (s *Store) syncFactDerived() error {
	if s == nil {
		return fmt.Errorf("wiki: nil store")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.factMu.Lock()
	defer s.factMu.Unlock()
	if projectionError := s.syncFactDerivedLocked(); projectionError != "" {
		return fmt.Errorf("wiki: sync fact projections: %s", projectionError)
	}
	return nil
}

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

func (s *Store) factTime(explicit time.Time) time.Time {
	if !explicit.IsZero() {
		return explicit.UTC()
	}
	if s.factNow != nil {
		return s.factNow().UTC()
	}
	return time.Now().UTC()
}

func (s *Store) resolveFactMutationLocked(input FactInput, now time.Time) FactMutation {
	identity := factIdentity(input.Subject, input.Key)
	revision := s.factState.Revision + 1
	basisAtMs := int64(0)
	if !input.BasisAt.IsZero() {
		basisAtMs = input.BasisAt.UTC().UnixMilli()
	}
	claim := FactClaim{
		Subject: input.Subject, Key: input.Key, Value: input.Value,
		Kind: input.Kind, Authority: input.Authority,
		Sources: input.Sources, Actor: input.Actor, Reason: input.Reason,
		RecordedAtMs: now.UnixMilli(), BasisAtMs: basisAtMs, Revision: revision,
	}
	claim.ID = newFactOperationID(revision, identity, input.Value, claim.RecordedAtMs)

	claims := s.factState.Facts[identity]
	active := make([]FactClaim, 0, len(claims))
	for i := range claims {
		switch claims[i].Status {
		case FactStatusCurrent, FactStatusConflicted:
			active = append(active, claims[i])
		case FactStatusSuperseded, FactStatusTombstoned:
		}
	}
	tombstoneBarrier := strongestFactTombstone(claims)
	updates := map[string]FactStatus{}
	resolution := "create"
	op := "assert"
	incomingRank := factAuthorityRank(input.Kind, input.Authority)

	if len(active) == 0 && tombstoneBarrier != nil {
		if incomingRank < tombstoneBarrier.rank ||
			(incomingRank == tombstoneBarrier.rank &&
				(!factUsesLatestPolicy(input.Kind, input.Authority) || factBasisTime(claim) <= tombstoneBarrier.atMs)) {
			claim.Status = FactStatusSuperseded
			resolution = "blocked_by_tombstone"
		}
	}
	if len(active) > 0 {
		maxRank := 0
		maxBasis := int64(0)
		sameValue := false
		for _, old := range active {
			rank := factAuthorityRank(old.Kind, old.Authority)
			if rank > maxRank {
				maxRank = rank
				maxBasis = factBasisTime(old)
			} else if rank == maxRank && factBasisTime(old) > maxBasis {
				maxBasis = factBasisTime(old)
			}
			if sameFactValue(old.Value, input.Value) {
				sameValue = true
			}
		}

		switch {
		case incomingRank > maxRank:
			claim.Status = FactStatusCurrent
			resolution = "higher_authority"
			markActiveFactStatuses(active, updates, FactStatusSuperseded)
		case incomingRank < maxRank:
			claim.Status = FactStatusSuperseded
			resolution = "ignored_lower_authority"
		case sameValue:
			claim.Status = FactStatusCurrent
			resolution = "reaffirmed"
			op = "reaffirm"
			markActiveFactStatuses(active, updates, FactStatusSuperseded)
		case factUsesLatestPolicy(input.Kind, input.Authority) && factBasisTime(claim) >= maxBasis:
			claim.Status = FactStatusCurrent
			resolution = "latest_authoritative"
			markActiveFactStatuses(active, updates, FactStatusSuperseded)
		case factUsesLatestPolicy(input.Kind, input.Authority):
			claim.Status = FactStatusSuperseded
			resolution = "ignored_older_basis"
		default:
			claim.Status = FactStatusConflicted
			resolution = "unresolved_conflict"
			markActiveFactStatuses(active, updates, FactStatusConflicted)
		}
	} else if claim.Status == "" {
		claim.Status = FactStatusCurrent
	}
	if len(updates) == 0 {
		updates = nil
	}
	return FactMutation{
		SchemaVersion: factSchemaVersion, Revision: revision,
		OperationID: claim.ID, AtMs: claim.RecordedAtMs, Op: op,
		Identity: identity, Claim: &claim, StatusUpdates: updates,
		Resolution: resolution, Reason: input.Reason,
	}
}

func factKindForIdentity(incoming FactKind, claims []FactClaim) (FactKind, error) {
	established := FactKind("")
	for _, claim := range claims {
		if claim.Kind == FactKindGeneric || claim.Kind == "" {
			continue
		}
		if established == "" {
			established = claim.Kind
			continue
		}
		if claim.Kind != established {
			return "", fmt.Errorf("fact identity history mixes kinds %q and %q", established, claim.Kind)
		}
	}
	if established == "" {
		if incoming != FactKindGeneric {
			for _, claim := range claims {
				if claim.Kind == FactKindGeneric && strings.TrimSpace(claim.Value) != "" && claim.Authority != FactAuthorityLegacyImport {
					return "", fmt.Errorf("only legacy generic facts may be promoted to kind %q", incoming)
				}
			}
		}
		return incoming, nil
	}
	if incoming == FactKindGeneric {
		return established, nil
	}
	if incoming != established {
		return "", fmt.Errorf("fact identity kind is %q, not %q", established, incoming)
	}
	return established, nil
}

func (s *Store) resolveFactTombstoneLocked(input FactTombstoneInput, now time.Time) FactMutation {
	identity := factIdentity(input.Subject, input.Key)
	revision := s.factState.Revision + 1
	claims := s.factState.Facts[identity]
	kind := FactKindGeneric
	updates := map[string]FactStatus{}
	activeCount := 0
	for i := len(claims) - 1; i >= 0; i-- {
		if claims[i].Kind != "" {
			kind = claims[i].Kind
			break
		}
	}
	incomingRank := factAuthorityRank(kind, input.Authority)
	requiredRank := 0
	for _, claim := range claims {
		if claim.Status != FactStatusCurrent && claim.Status != FactStatusConflicted && claim.Status != FactStatusTombstoned {
			continue
		}
		rank := factAuthorityRank(claim.Kind, claim.Authority)
		if rank > requiredRank {
			requiredRank = rank
		}
		if claim.Status == FactStatusCurrent || claim.Status == FactStatusConflicted {
			activeCount++
		}
	}
	claim := FactClaim{
		Subject: input.Subject, Key: input.Key, Kind: kind,
		Authority: input.Authority, Status: FactStatusTombstoned,
		Sources: input.Sources, Actor: input.Actor, Reason: input.Reason,
		RecordedAtMs: now.UnixMilli(), Revision: revision,
	}
	claim.ID = newFactOperationID(revision, identity, "<tombstone>", claim.RecordedAtMs)
	resolution := "tombstoned"
	if incomingRank < requiredRank && input.Authority != FactAuthorityDirectUser {
		// Lower-authority deletes are retained for audit but cannot retire a
		// stronger live claim. In particular, an agent_confirmed tool call must not
		// erase a direct-user fact merely because it is an explicit lifecycle action.
		// DirectUser is the sole exception: it is minted only by the trusted privacy
		// forget path, where an explicit user deletion must override document/runtime
		// authority regardless of rank.
		claim.Status = FactStatusSuperseded
		resolution = "ignored_lower_authority"
	} else {
		for _, old := range claims {
			if old.Status == FactStatusCurrent || old.Status == FactStatusConflicted {
				updates[old.ID] = FactStatusTombstoned
			}
		}
		if activeCount == 0 {
			resolution = "already_absent"
		}
	}
	if len(updates) == 0 {
		updates = nil
	}
	return FactMutation{
		SchemaVersion: factSchemaVersion, Revision: revision,
		OperationID: claim.ID, AtMs: claim.RecordedAtMs, Op: "tombstone",
		Identity: identity, Claim: &claim, StatusUpdates: updates,
		Resolution: resolution, Reason: input.Reason,
	}
}

func sameFactValue(left, right string) bool {
	left = strings.ToLower(strings.Join(strings.Fields(left), " "))
	right = strings.ToLower(strings.Join(strings.Fields(right), " "))
	return left == right
}

func markActiveFactStatuses(active []FactClaim, updates map[string]FactStatus, status FactStatus) {
	for _, old := range active {
		updates[old.ID] = status
	}
}

type factTombstoneBarrier struct {
	claim FactClaim
	rank  int
	atMs  int64
}

func strongestFactTombstone(claims []FactClaim) *factTombstoneBarrier {
	var barrier *factTombstoneBarrier
	for i := range claims {
		if claims[i].Status != FactStatusTombstoned {
			continue
		}
		rank := factAuthorityRank(claims[i].Kind, claims[i].Authority)
		atMs := factBasisTime(claims[i])
		if barrier == nil {
			barrier = &factTombstoneBarrier{claim: claims[i], rank: rank, atMs: atMs}
			continue
		}
		if atMs > barrier.atMs {
			barrier.atMs = atMs
		}
		if rank > barrier.rank ||
			(rank == barrier.rank && (atMs > factBasisTime(barrier.claim) ||
				(atMs == factBasisTime(barrier.claim) && claims[i].Revision > barrier.claim.Revision))) {
			barrier.claim = claims[i]
			barrier.rank = rank
		}
	}
	return barrier
}

type factJournalAppendOutcome uint8

const (
	factJournalAppendNotStarted factJournalAppendOutcome = iota
	factJournalAppendAmbiguous
	factJournalAppendCommitted
)

func (s *Store) appendFactMutationLocked(mutation FactMutation) error {
	if s.factJournalPoisoned != "" {
		return fmt.Errorf("wiki: fact journal requires restart before another mutation: %s", s.factJournalPoisoned)
	}
	raw, err := json.Marshal(mutation)
	if err != nil {
		return fmt.Errorf("wiki: marshal fact mutation: %w", err)
	}
	path := filepath.Join(s.dir, factJournalFile)
	raw = append(raw, '\n')
	record := raw
	appendRecord := s.factJournalAppend
	if appendRecord == nil {
		appendRecord = appendFactJournalRecord
	}
	outcome, appendErr := appendRecord(path, record)
	switch outcome {
	case factJournalAppendCommitted:
		if appendErr != nil {
			// File data reached fsync, which is the canonical commit point. A
			// subsequent close error cannot make callers retry the same revision.
			slog.Warn("fact journal close failed after durable commit",
				"revision", mutation.Revision, "error", appendErr)
		}
		return nil
	case factJournalAppendAmbiguous:
		if appendErr == nil {
			appendErr = fmt.Errorf("unknown append failure")
		}
		s.factJournalPoisoned = appendErr.Error()
		fatalErr := fmt.Errorf("wiki: fact journal append is ambiguous; restart required: %w", appendErr)
		if observer := s.factJournalFailureObserver; observer != nil {
			observer(fatalErr)
		}
		return fatalErr
	default:
		if appendErr == nil {
			appendErr = fmt.Errorf("fact journal append did not start")
		}
		return appendErr
	}
}

func appendFactJournalRecord(path string, record []byte) (factJournalAppendOutcome, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return factJournalAppendNotStarted, fmt.Errorf("wiki: open fact journal: %w", err)
	}
	if n, err := f.Write(record); err != nil {
		_ = f.Close()
		return factJournalAppendAmbiguous, fmt.Errorf("wiki: append fact journal: %w", err)
	} else if n != len(record) {
		_ = f.Close()
		return factJournalAppendAmbiguous, fmt.Errorf("wiki: append fact journal: short write %d/%d", n, len(record))
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return factJournalAppendAmbiguous, fmt.Errorf("wiki: sync fact journal: %w", err)
	}
	if err := f.Close(); err != nil {
		return factJournalAppendCommitted, fmt.Errorf("wiki: close fact journal: %w", err)
	}
	return factJournalAppendCommitted, nil
}

func ensureFactJournal(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return fmt.Errorf("create fact journal: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync new fact journal: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close new fact journal: %w", err)
	}
	if err := syncFactParentDir(path); err != nil {
		return fmt.Errorf("sync new fact journal directory: %w", err)
	}
	return nil
}

func syncFactParentDir(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}

func (s *Store) validateFactMutationLocked(mutation FactMutation) error {
	if mutation.SchemaVersion != factSchemaVersion {
		return fmt.Errorf("unsupported schema version %d", mutation.SchemaVersion)
	}
	if mutation.Revision != s.factState.Revision+1 {
		return fmt.Errorf("revision gap: have %d, mutation %d", s.factState.Revision, mutation.Revision)
	}
	switch mutation.Op {
	case "assert", "reaffirm", "tombstone":
	default:
		return fmt.Errorf("unsupported operation %q", mutation.Op)
	}
	if mutation.Claim == nil {
		return fmt.Errorf("operation %q has no claim", mutation.Op)
	}
	claim := mutation.Claim
	if err := validateFactClaimBounds(claim); err != nil {
		return err
	}
	if err := validateFactRuneLimit("mutation reason", mutation.Reason, factAuditMaxRunes); err != nil {
		return err
	}
	if mutation.Identity == "" || !factMutationIdentityMatchesClaim(mutation.Identity, *claim) {
		return fmt.Errorf("claim identity mismatch %q", mutation.Identity)
	}
	canonicalIdentity := factIdentity(claim.Subject, claim.Key)
	if claim.Subject != normalizeFactSubject(claim.Subject) || claim.Key != normalizeFactKey(claim.Key) {
		return fmt.Errorf("claim identity is not canonical")
	}
	if mutation.OperationID == "" || mutation.OperationID != claim.ID {
		return fmt.Errorf("operation id does not match claim id")
	}
	idValue := claim.Value
	if mutation.Op == "tombstone" {
		idValue = "<tombstone>"
	}
	if expected := newFactOperationID(mutation.Revision, mutation.Identity, idValue, mutation.AtMs); claim.ID != expected {
		return fmt.Errorf("claim id does not match mutation contents")
	}
	if mutation.Reason != claim.Reason {
		return fmt.Errorf("mutation reason does not match claim reason")
	}
	if claim.Revision != mutation.Revision {
		return fmt.Errorf("claim revision %d does not match mutation %d", claim.Revision, mutation.Revision)
	}
	if claim.RecordedAtMs != mutation.AtMs {
		return fmt.Errorf("claim timestamp does not match mutation timestamp")
	}
	if !isKnownFactKind(claim.Kind) {
		return fmt.Errorf("unknown fact kind %q", claim.Kind)
	}
	if !isKnownFactAuthority(claim.Authority) {
		return fmt.Errorf("unknown fact authority %q", claim.Authority)
	}
	if !isKnownFactStatus(claim.Status) {
		return fmt.Errorf("unknown fact status %q", claim.Status)
	}
	canonicalKind, err := factKindForIdentity(claim.Kind, s.factState.Facts[canonicalIdentity])
	if err != nil {
		return err
	}
	if canonicalKind != claim.Kind {
		return fmt.Errorf("fact identity kind downgrade from %q to generic", canonicalKind)
	}
	if mutation.Op != "tombstone" && factRequiresBasisAt(claim.Kind, claim.Authority) && claim.BasisAtMs == 0 {
		return fmt.Errorf("primary_document %s claim has no basis time", claim.Kind)
	}
	if mutation.Op == "tombstone" {
		if strings.TrimSpace(claim.Value) != "" {
			return fmt.Errorf("tombstone claim contains a value")
		}
		if claim.Status != FactStatusTombstoned && claim.Status != FactStatusSuperseded {
			return fmt.Errorf("tombstone claim has status %q", claim.Status)
		}
	} else {
		if strings.TrimSpace(claim.Value) == "" {
			return fmt.Errorf("assertion claim has no value")
		}
		if claim.Status == FactStatusTombstoned {
			return fmt.Errorf("assertion claim is tombstoned")
		}
	}
	for _, claims := range s.factState.Facts {
		for _, existing := range claims {
			if existing.ID == claim.ID {
				return fmt.Errorf("duplicate claim id %q", claim.ID)
			}
		}
	}
	for claimID, status := range mutation.StatusUpdates {
		if !isKnownFactStatus(status) {
			return fmt.Errorf("status update for %q has unknown status %q", claimID, status)
		}
		if mutation.Op == "tombstone" && status != FactStatusTombstoned {
			return fmt.Errorf("tombstone update for %q has status %q", claimID, status)
		}
		if mutation.Op != "tombstone" && status != FactStatusSuperseded && status != FactStatusConflicted {
			return fmt.Errorf("assertion update for %q has status %q", claimID, status)
		}
		if _, found := s.factStatusUpdateIdentityLocked(mutation, claimID); !found {
			return fmt.Errorf("status update references unknown claim %q", claimID)
		}
	}
	return nil
}

// factStatusUpdateIdentityLocked resolves the canonical bucket containing an
// older claim named by mutation.StatusUpdates. New length-prefixed identities
// may update only their own tuple. A schema-v1 delimiter identity may also name
// a prior claim from another tuple that collided into the same legacy history.
// Validation accepts that historical row so startup remains possible; apply
// ignores the cross-tuple status change because it was an artifact of the old
// ambiguous key, while the immutable journal retains the original audit event.
func (s *Store) factStatusUpdateIdentityLocked(mutation FactMutation, claimID string) (string, bool) {
	canonicalIdentity := factIdentity(mutation.Claim.Subject, mutation.Claim.Key)
	for _, claim := range s.factState.Facts[canonicalIdentity] {
		if claim.ID == claimID {
			return canonicalIdentity, true
		}
	}
	if mutation.Identity != legacyFactIdentity(mutation.Claim.Subject, mutation.Claim.Key) {
		return "", false
	}
	for _, identity := range sortedFactIdentities(s.factState.Facts) {
		if identity == canonicalIdentity {
			continue
		}
		for _, claim := range s.factState.Facts[identity] {
			if claim.ID == claimID && legacyFactIdentity(claim.Subject, claim.Key) == mutation.Identity {
				return identity, true
			}
		}
	}
	return "", false
}

func (s *Store) applyFactMutationLocked(mutation FactMutation) error {
	if err := s.validateFactMutationLocked(mutation); err != nil {
		return err
	}
	if s.factState.Facts == nil {
		s.factState.Facts = make(map[string][]FactClaim)
	}
	// The journal identity is part of the immutable operation-ID input. Keep it
	// untouched for verification, but always store state under the canonical
	// tuple key so legacy delimiter collisions cannot merge two claim histories
	// during replay.
	canonicalIdentity := factIdentity(mutation.Claim.Subject, mutation.Claim.Key)
	statusClaimIDs := make([]string, 0, len(mutation.StatusUpdates))
	for claimID := range mutation.StatusUpdates {
		statusClaimIDs = append(statusClaimIDs, claimID)
	}
	sort.Strings(statusClaimIDs)
	for _, claimID := range statusClaimIDs {
		identity, found := s.factStatusUpdateIdentityLocked(mutation, claimID)
		if !found {
			return fmt.Errorf("status update references unknown claim %q", claimID)
		}
		if identity != canonicalIdentity {
			// Schema-v1 considered distinct tuples equal when their colon-joined
			// identities collided. Do not carry that accidental supersession into
			// the length-prefixed state; the old mutation remains in the journal.
			continue
		}
		claims := append([]FactClaim(nil), s.factState.Facts[identity]...)
		for i := range claims {
			if claims[i].ID == claimID {
				claims[i].Status = mutation.StatusUpdates[claimID]
				break
			}
		}
		s.factState.Facts[identity] = claims
	}
	if mutation.Claim != nil {
		claims := append([]FactClaim(nil), s.factState.Facts[canonicalIdentity]...)
		claims = append(claims, cloneFactClaim(*mutation.Claim))
		s.factState.Facts[canonicalIdentity] = claims
	}
	s.factState.SchemaVersion = factSchemaVersion
	s.factState.Revision = mutation.Revision
	return nil
}

func (s *Store) loadFactPlane() error {
	s.factMu.Lock()
	defer s.factMu.Unlock()
	s.factState = FactSnapshot{SchemaVersion: factSchemaVersion, Facts: make(map[string][]FactClaim)}

	snapshotPath := filepath.Join(s.dir, factStateFile)
	snapshotRevision, snapshotExists, snapshotErr := factSnapshotWatermark(snapshotPath)
	journalPath := filepath.Join(s.dir, factJournalFile)
	raw, err := os.ReadFile(journalPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read fact journal: %w", err)
		}
		// A derived snapshot without its permanent history is not safe to trust.
		if snapshotExists && snapshotErr != nil {
			return fmt.Errorf("fact journal missing and snapshot is unreadable: %w", snapshotErr)
		}
		if snapshotRevision > 0 {
			return fmt.Errorf("fact journal missing for snapshot revision %d", snapshotRevision)
		}
		return ensureFactJournal(journalPath)
	}

	lines := bytes.Split(raw, []byte{'\n'})
	terminated := len(raw) == 0 || raw[len(raw)-1] == '\n'
	recoveredTail := false
	offset := 0
	for i, line := range lines {
		lineStart := offset
		offset += len(line) + 1
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var mutation FactMutation
		if err := json.Unmarshal(line, &mutation); err != nil {
			lastNonEmpty := true
			for _, tail := range lines[i+1:] {
				if len(bytes.TrimSpace(tail)) > 0 {
					lastNonEmpty = false
					break
				}
			}
			if !lastNonEmpty || terminated {
				return fmt.Errorf("corrupt fact journal line %d: %w", i+1, err)
			}
			// Only an incomplete trailing record is recoverable. Remove it before
			// the next append so it cannot concatenate with a valid JSON row.
			if err := truncateFactJournal(journalPath, int64(lineStart)); err != nil {
				return fmt.Errorf("truncate torn fact journal tail: %w", err)
			}
			recoveredTail = true
			slog.Warn("truncated torn fact journal tail", "line", i+1)
			break
		}
		if err := s.applyFactMutationLocked(mutation); err != nil {
			return fmt.Errorf("replay fact journal line %d: %w", i+1, err)
		}
	}
	if snapshotExists && snapshotErr == nil && snapshotRevision > s.factState.Revision {
		return fmt.Errorf("fact journal revision %d is behind snapshot watermark %d", s.factState.Revision, snapshotRevision)
	}
	if !recoveredTail && len(raw) > 0 && !terminated {
		if err := terminateFactJournal(journalPath); err != nil {
			return fmt.Errorf("terminate fact journal: %w", err)
		}
	}
	if s.factState.Revision > 0 || (snapshotExists && snapshotErr != nil) {
		if err := s.saveFactSnapshotLocked(); err != nil {
			return fmt.Errorf("checkpoint fact snapshot: %w", err)
		}
	}
	// Older binaries created the journal lazily on the first mutation. Sync its
	// directory entry during startup so future append commit semantics never
	// depend on an unproven create from a previous process.
	if err := syncFactParentDir(journalPath); err != nil {
		return fmt.Errorf("sync existing fact journal directory: %w", err)
	}
	return nil
}

func factSnapshotWatermark(path string) (FactRevision, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, true, err
	}
	var snapshot FactSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return 0, true, err
	}
	if snapshot.SchemaVersion != factSchemaVersion {
		return 0, true, fmt.Errorf("unsupported snapshot schema version %d", snapshot.SchemaVersion)
	}
	return snapshot.Revision, true, nil
}

func truncateFactJournal(path string, size int64) error {
	if err := os.Truncate(path, size); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func terminateFactJournal(path string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte{'\n'}); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func (s *Store) saveFactSnapshotLocked() error {
	raw, err := json.MarshalIndent(s.factState, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal fact snapshot: %w", err)
	}
	path := filepath.Join(s.dir, factStateFile)
	tmp := path + ".tmp"
	if err := writeFileSync(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncFactParentDir(path)
}

func cloneFactSnapshot(in FactSnapshot) FactSnapshot {
	out := FactSnapshot{SchemaVersion: in.SchemaVersion, Revision: in.Revision, Facts: make(map[string][]FactClaim, len(in.Facts))}
	for identity, claims := range in.Facts {
		copyClaims := make([]FactClaim, len(claims))
		for i := range claims {
			copyClaims[i] = cloneFactClaim(claims[i])
		}
		out.Facts[identity] = copyClaims
	}
	return out
}

func cloneFactClaim(in FactClaim) FactClaim {
	in.Sources = append([]string(nil), in.Sources...)
	return in
}
