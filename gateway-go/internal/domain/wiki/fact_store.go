package wiki

import (
	"encoding/json"
	"errors"
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
	s.maybeRotateFactJournalLocked()
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
	s.maybeRotateFactJournalLocked()
	return result, nil
}

func (s *Store) syncFactDerivedLocked() string {
	var projectionErrors []string
	aheadOnly := true
	record := func(prefix string, err error) {
		if err == nil {
			return
		}
		if !errors.Is(err, ErrFactProjectionAhead) {
			aheadOnly = false
		}
		projectionErrors = append(projectionErrors, prefix+err.Error())
	}
	record("snapshot: ", s.saveFactSnapshotLocked())
	record("wiki: ", s.syncFactPageLocked())
	record("workspace: ", s.syncFactWorkspaceLocked())
	s.factProjectionError = strings.Join(projectionErrors, "; ")
	s.factProjectionAheadOnly = len(projectionErrors) > 0 && aheadOnly
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

func (s *Store) factTime(explicit time.Time) time.Time {
	if !explicit.IsZero() {
		return explicit.UTC()
	}
	if s.factNow != nil {
		return s.factNow().UTC()
	}
	return time.Now().UTC()
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
		s.factJournalRecords++
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
	if _, duplicate := s.factClaimIDs[claim.ID]; duplicate {
		return fmt.Errorf("duplicate claim id %q", claim.ID)
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
		if s.factClaimIDs == nil {
			s.factClaimIDs = make(map[string]struct{})
		}
		s.factClaimIDs[mutation.Claim.ID] = struct{}{}
	}
	s.factState.SchemaVersion = factSchemaVersion
	s.factState.Revision = mutation.Revision
	return nil
}

func (s *Store) loadFactPlane() error {
	s.factMu.Lock()
	defer s.factMu.Unlock()
	s.factState = FactSnapshot{SchemaVersion: factSchemaVersion, Facts: make(map[string][]FactClaim)}
	s.factClaimIDs = make(map[string]struct{})
	s.factJournalRecords = 0

	snapshotPath := filepath.Join(s.dir, factStateFile)
	journalPath := filepath.Join(s.dir, factJournalFile)
	snapshot, snapshotErr := readFactSnapshot(snapshotPath)
	snapshotExists := snapshotErr == nil || !os.IsNotExist(snapshotErr)
	if snapshotErr != nil && os.IsNotExist(snapshotErr) {
		snapshotErr = nil
		snapshotExists = false
	}
	// Seeding from the snapshot is what bounds startup: only the active segment
	// is replayed on top of it. Without a usable snapshot the plane is rebuilt
	// from the full archived ledger instead — slower, but never guessy.
	seeded := snapshotExists && snapshotErr == nil
	snapshotRevision := FactRevision(0)
	if seeded {
		s.factState = canonicalizeFactSnapshot(snapshot)
		s.indexFactClaimIDsLocked()
		snapshotRevision = snapshot.Revision
	}

	if _, err := os.Stat(journalPath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat fact journal: %w", err)
		}
		archives, archiveErr := factJournalArchives(s.dir)
		if archiveErr != nil && !os.IsNotExist(archiveErr) {
			return fmt.Errorf("list fact journal archives: %w", archiveErr)
		}
		if len(archives) == 0 {
			// A derived snapshot without its permanent history is not safe to trust.
			if snapshotExists && snapshotErr != nil {
				return fmt.Errorf("fact journal missing and snapshot is unreadable: %w", snapshotErr)
			}
			if snapshotRevision > 0 {
				return fmt.Errorf("fact journal missing for snapshot revision %d", snapshotRevision)
			}
			return ensureFactJournal(journalPath)
		}
		// Rotation renames the active segment and then creates a new one. If that
		// create failed, the archives still hold every revision and the missing
		// active segment is an empty file waiting to be made. Recreate it and let
		// the ordinary path below rebuild and re-verify: replaying an empty
		// segment is a no-op, and the watermark check still refuses if the
		// archives do not reach the snapshot.
		slog.Warn("recreating the fact journal segment lost after rotation",
			"snapshotRevision", snapshotRevision, "archives", len(archives))
		if err := ensureFactJournal(journalPath); err != nil {
			return err
		}
	}

	if !seeded {
		if err := s.replayFactArchivesLocked(); err != nil {
			return err
		}
	}
	records, journalRevision, err := s.replayFactJournalFileLocked(journalPath, true)
	if err != nil {
		return err
	}
	s.factJournalRecords = records
	// Seeding hides a rolled-back journal — state stays at the snapshot revision
	// no matter how much history the file lost — so prove the durable ledger
	// still reaches the watermark. Archived segments count: rotation snapshots
	// before it renames, so the newest archive name is history already proven.
	if archived := newestFactArchiveRevision(s.dir); archived > journalRevision {
		journalRevision = archived
	}
	if journalRevision < snapshotRevision {
		return fmt.Errorf("fact journal revision %d is behind snapshot watermark %d",
			journalRevision, snapshotRevision)
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
