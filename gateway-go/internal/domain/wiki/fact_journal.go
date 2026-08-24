package wiki

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Journal segmentation.
//
// The fact journal is permanent history: nothing in it is ever rewritten or
// deleted. But replaying all of it on every boot makes startup grow without
// bound, so the file is SEGMENTED instead of compacted. Once the active segment
// passes factJournalRotateRecords, the durable snapshot is refreshed and the
// segment is renamed to an archive named after the revision it ends at. Startup
// then seeds state from the snapshot and replays only the active segment.
//
// Archives are audit history. They are read again only on the repair path,
// where the snapshot is missing or unreadable and the plane has to be rebuilt
// from the beginning — that is when the full ledger earns its keep.
//
// Every function here assumes the caller holds factMu (see the Locked suffix on
// the methods); the free functions touch only their path argument.
const (
	factJournalArchivePrefix = ".fact-mutations."
	factJournalArchiveSuffix = ".jsonl"
	// Zero-padded so a lexical sort is a revision sort.
	factJournalArchiveFormat = factJournalArchivePrefix + "%020d" + factJournalArchiveSuffix
	factJournalRotateRecords = 2000
)

func factJournalArchiveName(revision FactRevision) string {
	return fmt.Sprintf(factJournalArchiveFormat, uint64(revision))
}

// factJournalArchives lists archived segments in ascending revision order.
func factJournalArchives(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == factJournalFile || !strings.HasPrefix(name, factJournalArchivePrefix) ||
			!strings.HasSuffix(name, factJournalArchiveSuffix) {
			continue
		}
		middle := strings.TrimSuffix(strings.TrimPrefix(name, factJournalArchivePrefix), factJournalArchiveSuffix)
		if middle == "" || strings.Trim(middle, "0123456789") != "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// maybeRotateFactJournalLocked archives the active segment once it is long
// enough, but only after the snapshot that lets startup skip it is durable.
// Rotation is best-effort: a failure leaves the segment in place, which costs
// startup time and never costs a mutation.
func (s *Store) maybeRotateFactJournalLocked() {
	threshold := factJournalRotateRecords
	if s.factJournalRotateAt > 0 {
		threshold = s.factJournalRotateAt
	}
	if s.factJournalRecords < threshold || s.factJournalPoisoned != "" {
		return
	}
	// The snapshot is the only thing that makes an archived segment skippable.
	// Refresh it here rather than trusting a projection sync that may have been
	// deferred or degraded.
	if err := s.saveFactSnapshotLocked(); err != nil {
		slog.Warn("fact journal rotation skipped: snapshot not durable", "error", err)
		return
	}
	journalPath := filepath.Join(s.dir, factJournalFile)
	archivePath := filepath.Join(s.dir, factJournalArchiveName(s.factState.Revision))
	if _, err := os.Stat(archivePath); err == nil {
		slog.Warn("fact journal rotation skipped: archive already exists", "archive", filepath.Base(archivePath))
		return
	}
	if err := os.Rename(journalPath, archivePath); err != nil {
		slog.Warn("fact journal rotation skipped: rename failed", "error", err)
		return
	}
	if err := ensureFactJournal(journalPath); err != nil {
		// The rename already committed, so history is intact, but the next
		// mutation has nowhere to append until this is repaired: that is durable
		// write loss for the user, not a recoverable hiccup.
		slog.Error("fact journal rotated but the new segment could not be created",
			"error", err, "revision", s.factState.Revision)
		return
	}
	if err := syncFactParentDir(journalPath); err != nil {
		slog.Warn("fact journal rotation could not sync its directory entry", "error", err)
	}
	s.factJournalRecords = 0
	slog.Info("fact journal segment archived",
		"revision", s.factState.Revision, "archive", filepath.Base(archivePath))
}

// replayFactJournalFileLocked applies one segment. Only the ACTIVE segment can
// hold a torn trailing record, so tail recovery is limited to it; a truncated
// archive is corruption and must fail loudly.
func (s *Store) replayFactJournalFileLocked(path string, active bool) (records int, maxRevision FactRevision, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	lines := strings.Split(string(raw), "\n")
	terminated := len(raw) == 0 || raw[len(raw)-1] == '\n'
	offset := 0
	for i, line := range lines {
		lineStart := offset
		offset += len(line) + 1
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var mutation FactMutation
		if unmarshalErr := json.Unmarshal([]byte(line), &mutation); unmarshalErr != nil {
			lastNonEmpty := true
			for _, tail := range lines[i+1:] {
				if strings.TrimSpace(tail) != "" {
					lastNonEmpty = false
					break
				}
			}
			if !active || !lastNonEmpty || terminated {
				return records, maxRevision, fmt.Errorf("corrupt fact journal line %d in %s: %w",
					i+1, filepath.Base(path), unmarshalErr)
			}
			// Only an incomplete trailing record is recoverable. Remove it before
			// the next append so it cannot concatenate with a valid JSON row.
			if truncateErr := truncateFactJournal(path, int64(lineStart)); truncateErr != nil {
				return records, maxRevision, fmt.Errorf("truncate torn fact journal tail: %w", truncateErr)
			}
			slog.Warn("truncated torn fact journal tail", "line", i+1)
			return records, maxRevision, nil
		}
		records++
		if mutation.Revision > maxRevision {
			maxRevision = mutation.Revision
		}
		if mutation.Revision <= s.factState.Revision {
			// Already folded into the snapshot this replay was seeded from.
			continue
		}
		if applyErr := s.applyFactMutationLocked(mutation); applyErr != nil {
			return records, maxRevision, fmt.Errorf("replay fact journal line %d in %s: %w",
				i+1, filepath.Base(path), applyErr)
		}
	}
	if active && !terminated && len(raw) > 0 {
		if err := terminateFactJournal(path); err != nil {
			return records, maxRevision, fmt.Errorf("terminate fact journal: %w", err)
		}
	}
	return records, maxRevision, nil
}

// replayFactArchivesLocked rebuilds pre-snapshot history from archived
// segments. Used only when the snapshot cannot seed the plane.
func (s *Store) replayFactArchivesLocked() error {
	archives, err := factJournalArchives(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("list fact journal archives: %w", err)
	}
	for _, name := range archives {
		if _, _, err := s.replayFactJournalFileLocked(filepath.Join(s.dir, name), false); err != nil {
			return err
		}
	}
	return nil
}

// canonicalizeFactSnapshot rekeys a snapshot by the canonical identity of each
// claim. A snapshot written by a schema-v1 binary is keyed by the ambiguous
// colon-joined identity; seeding from it verbatim would keep two colliding
// tuples merged and would hide a legacy history from its own subject/key. This
// applies the same rekey that journal replay performs, so seeding and replay
// converge on identical state.
func canonicalizeFactSnapshot(in FactSnapshot) FactSnapshot {
	out := FactSnapshot{
		SchemaVersion: factSchemaVersion,
		Revision:      in.Revision,
		Facts:         make(map[string][]FactClaim, len(in.Facts)),
	}
	for _, identity := range sortedFactIdentities(in.Facts) {
		for _, claim := range in.Facts[identity] {
			canonical := factIdentity(claim.Subject, claim.Key)
			out.Facts[canonical] = append(out.Facts[canonical], cloneFactClaim(claim))
		}
	}
	for identity, claims := range out.Facts {
		sort.SliceStable(claims, func(i, j int) bool { return claims[i].Revision < claims[j].Revision })
		out.Facts[identity] = claims
	}
	return out
}

// newestFactArchiveRevision reports the revision the most recent archived
// segment ends at, or 0 when nothing has been archived. Rotation writes the
// snapshot before renaming, so this is the floor of history the active segment
// no longer has to prove.
func newestFactArchiveRevision(dir string) FactRevision {
	archives, err := factJournalArchives(dir)
	if err != nil || len(archives) == 0 {
		return 0
	}
	name := archives[len(archives)-1]
	middle := strings.TrimSuffix(strings.TrimPrefix(name, factJournalArchivePrefix), factJournalArchiveSuffix)
	var revision uint64
	if _, err := fmt.Sscanf(middle, "%d", &revision); err != nil {
		return 0
	}
	return FactRevision(revision)
}

func readFactSnapshot(path string) (FactSnapshot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return FactSnapshot{}, err
	}
	var snapshot FactSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return FactSnapshot{}, err
	}
	if snapshot.SchemaVersion != factSchemaVersion {
		return FactSnapshot{}, fmt.Errorf("unsupported snapshot schema version %d", snapshot.SchemaVersion)
	}
	if snapshot.Facts == nil {
		snapshot.Facts = make(map[string][]FactClaim)
	}
	return snapshot, nil
}

// indexFactClaimIDsLocked rebuilds the duplicate-ID index from current state.
func (s *Store) indexFactClaimIDsLocked() {
	ids := make(map[string]struct{}, len(s.factState.Facts))
	for _, claims := range s.factState.Facts {
		for _, claim := range claims {
			ids[claim.ID] = struct{}{}
		}
	}
	s.factClaimIDs = ids
}
