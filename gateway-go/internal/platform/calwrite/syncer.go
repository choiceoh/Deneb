package calwrite

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// remote is the narrow write surface the syncer needs; *Client is the live impl,
// and tests supply a fake so the id-mapping logic is exercised without HTTP.
type remote interface {
	Insert(ctx context.Context, localID string, ev calendar.Event) (string, error)
	Patch(ctx context.Context, googleID, localID string, ev calendar.Event) error
	Delete(ctx context.Context, googleID string) error
}

// Syncer mirrors local calendar mutations to Google and owns the localID→googleID
// map so an edit/delete of a local event targets the same Google event a prior
// create made. The map is persisted to {stateDir}/calendar-sync.json so the
// pairing survives restarts.
//
// Single-user, single-writer: one mutex guards both the id-map and the (rare)
// network call, so calendar syncs serialize. The one external callback (warn) is
// invoked only after the lock is released.
type Syncer struct {
	mu        sync.Mutex
	path      string
	ids       map[string]string   // localID → googleID
	cancelled map[string]struct{} // localIDs deleted before first mirror completed
	remote    remote
	warn      func(op string, err error) // best-effort failure sink (nil ok)
}

// NewSyncer loads the id-map from path (empty if absent) and wires the remote.
func NewSyncer(path string, r remote, warn func(op string, err error)) (*Syncer, error) {
	s := &Syncer{path: path, ids: map[string]string{}, cancelled: map[string]struct{}{}, remote: r, warn: warn}
	if _, err := jsonutil.LoadFile(path, &s.ids, "calwrite"); err != nil {
		return nil, err
	}
	if s.ids == nil {
		s.ids = map[string]string{}
	}
	return s, nil
}

// Push mirrors a local event to Google: PATCH when it is already mapped, else
// POST and remember the new Google id. The returned error is also handed to the
// warn sink; callers treat the mirror as best-effort and may ignore it (the
// local write is already the source of truth).
func (s *Syncer) Push(ctx context.Context, localID string, ev calendar.Event) error {
	s.mu.Lock()
	err := s.pushLocked(ctx, localID, ev)
	s.mu.Unlock()
	if err != nil {
		s.report("push", err)
	}
	return err
}

func (s *Syncer) pushLocked(ctx context.Context, localID string, ev calendar.Event) error {
	if _, skip := s.cancelled[localID]; skip {
		delete(s.cancelled, localID)
		return nil
	}
	if gid, ok := s.ids[localID]; ok {
		return s.remote.Patch(ctx, gid, localID, ev)
	}
	gid, err := s.remote.Insert(ctx, localID, ev)
	if err != nil {
		return err
	}
	// No second cancellation check here: Push holds s.mu across this Insert, so
	// Remove cannot mark the id mid-flight, and the check above already consumed
	// any pending cancellation. The race this guards against is ORDERING
	// (Remove runs before Push), not interleaving — and that is what the entry
	// check at the top of this function handles.
	s.ids[localID] = gid
	if err := s.persistLocked(); err != nil {
		delete(s.ids, localID)
		_ = s.remote.Delete(ctx, gid)
		return err
	}
	return nil
}

// Remove deletes the Google mirror of a local event and forgets the pairing. When
// the local id was never mirrored, the id is remembered so a concurrent Push skips
// inserting an orphan Google event.
func (s *Syncer) Remove(ctx context.Context, localID string) error {
	s.mu.Lock()
	err := s.removeLocked(ctx, localID)
	s.mu.Unlock()
	if err != nil {
		s.report("remove", err)
	}
	return err
}

func (s *Syncer) removeLocked(ctx context.Context, localID string) error {
	gid, ok := s.ids[localID]
	if !ok {
		s.cancelled[localID] = struct{}{}
		return nil
	}
	if err := s.remote.Delete(ctx, gid); err != nil {
		return err
	}
	delete(s.ids, localID)
	return s.persistLocked()
}

// MirroredGoogleIDs returns the set of Google event ids Deneb has authored, so
// the read-merge path can drop them when both read and write integrations are on
// (the local store already provides those events, so the Google copy is a dup).
func (s *Syncer) MirroredGoogleIDs() map[string]struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]struct{}, len(s.ids))
	for _, gid := range s.ids {
		out[gid] = struct{}{}
	}
	return out
}

func (s *Syncer) persistLocked() error {
	return jsonutil.WriteFile(s.path, s.ids, "calwrite")
}

func (s *Syncer) report(op string, err error) {
	if s.warn != nil {
		s.warn(op, err)
	}
}

var (
	globalMu     sync.Mutex
	globalSyncer *Syncer
)

// errWriteDisabled is returned by DefaultSyncer when the write mirror is turned
// off, so the handler degrades to local-only exactly as before the feature
// existed. The mirror is on by default; this fires only when explicitly disabled.
var errWriteDisabled = fmt.Errorf("Google Calendar 쓰기 동기화가 꺼져 있습니다 — 로컬 저장만 사용 (DENEB_CALENDAR_GOOGLE_WRITE=0 으로 비활성화됨)") //nolint:staticcheck // ST1005 — Korean operator-facing message

// DefaultSyncer returns the process-wide syncer, initializing on first call.
// Returns errWriteDisabled (so every caller falls back to local-only) unless the
// mirror is enabled and a credential/token pair is present.
func DefaultSyncer(warn func(op string, err error)) (*Syncer, error) {
	if !writeEnabled() {
		return nil, errWriteDisabled
	}
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalSyncer != nil {
		return globalSyncer, nil
	}
	c, err := newClient()
	if err != nil {
		return nil, err
	}
	s, err := NewSyncer(filepath.Join(config.ResolveStateDir(), "calendar-sync.json"), c, warn)
	if err != nil {
		return nil, err
	}
	globalSyncer = s
	return s, nil
}
