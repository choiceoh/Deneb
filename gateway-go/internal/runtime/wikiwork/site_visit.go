// site_visit.go — record a project 현장 visit from a phone location fix.
//
// The location stream (phoneevents location_update) was cache-only: it fed
// phone_read but never became memory, so "오늘 수산리 현장 다녀옴" evaporated.
// This recorder closes that gap with a strict privacy boundary: it records a
// visit ONLY when the geocoded place matches a project's 현장(sites) already in
// the wiki — Deneb never logs where you are generally, only that you visited a
// site it already tracks as work. Raw coordinates and non-matching places are
// never written anywhere by this path.
//
// The client sends an on-device reverse-geocoded place string in the
// location_update payload; matching is deterministic (Store.MatchProjectSite,
// sites-only so a same-named place can't false-trigger). Deduped per
// project-per-day so a stationary phone pinging every few minutes records one
// visit, not dozens.
package wikiwork

import (
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

const (
	// SiteVisitStateFile persists per-project-per-day dedup keys.
	SiteVisitStateFile = "site-visit-state.json"
	// siteVisitStateCap bounds the dedup set; oldest keys drop past the cap.
	siteVisitStateCap = 400
)

// SiteVisitRecorder appends a 방문 log op to a project when a location fix's
// geocoded place matches that project's 현장. Safe for concurrent Record calls.
type SiteVisitRecorder struct {
	store     *wiki.Store
	logger    *slog.Logger
	statePath string

	mu     sync.Mutex
	seen   map[string]int64 // "project|YYYY-MM-DD" → recorded unix millis
	loaded bool

	// persistMu serializes state-file writes so a stale snapshot can't clobber
	// a newer one (writes take a fresh snapshot under this lock — see persist).
	persistMu sync.Mutex
}

// NewSiteVisitRecorder constructs the recorder. store nil ⇒ Record no-ops.
func NewSiteVisitRecorder(store *wiki.Store, logger *slog.Logger, statePath string) *SiteVisitRecorder {
	return &SiteVisitRecorder{store: store, logger: logger, statePath: statePath, seen: map[string]int64{}}
}

// RecordFromLocationPayload parses the location_update JSON, extracts the
// geocoded place, and records a visit if it matches a known project 현장. No
// place field (older client) or no site match ⇒ silent no-op.
func (r *SiteVisitRecorder) RecordFromLocationPayload(payload string) {
	if r == nil || r.store == nil {
		return
	}
	var fix struct {
		Place string `json:"place"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(payload)), &fix) != nil {
		return
	}
	r.Record(strings.TrimSpace(fix.Place))
}

// Record matches place against project 현장 and, on a match not already logged
// today, appends a 방문 op to that project's 로그.md.
func (r *SiteVisitRecorder) Record(place string) {
	if r == nil || r.store == nil || place == "" {
		return
	}
	ref, matchedKey, ok := r.store.MatchProjectSite(place)
	if !ok {
		return // not a tracked 현장 — record nothing (privacy boundary)
	}
	// The wiki path keys off the FOLDER name (ref.Path segment), never the
	// display title: a project whose rep Title is "기아 화성" but folder is
	// 기아-화성 would otherwise write to 프로젝트/기아 화성/로그.md and orphan the
	// visit. display (Title, else folder) is only the human-readable heading.
	folder, ok := wiki.ProjectNameOf(ref.Path)
	if !ok || folder == "" {
		return
	}
	display := strings.TrimSpace(ref.Name)
	if display == "" {
		display = folder
	}

	now := dentime.Now()
	today := now.Format("2006-01-02")
	key := folder + "|" + today

	r.mu.Lock()
	r.ensureLoadedLocked()
	if _, done := r.seen[key]; done {
		r.mu.Unlock()
		return // already recorded a visit to this project today
	}
	r.seen[key] = now.UnixMilli()
	r.pruneLocked()
	r.mu.Unlock()

	// Sanitize third-party text before it reaches the markdown log: collapse
	// all whitespace/control chars to single spaces so a crafted payload can't
	// inject new headings or bullets (the Android escape is best-effort; this
	// is the server-side defense).
	section := "## [" + today + "] 방문 | " + sanitizeLogText(place) +
		"\n- 현장: " + sanitizeLogText(matchedKey) + " (휴대폰 위치 기반)\n"
	err := r.store.UpdatePage(wiki.LogPagePath(folder), func(cur *wiki.Page) (*wiki.Page, error) {
		if cur == nil {
			p := wiki.NewPage(display+" 진행 로그", "프로젝트", nil)
			p.Meta.Type = "log"
			p.Meta.Summary = display + " 진행 로그"
			p.Body = section
			return p, nil
		}
		cur.Body = strings.TrimRight(cur.Body, "\n") + "\n\n" + section
		cur.Meta.Updated = today
		return cur, nil
	})
	if err != nil {
		if r.logger != nil {
			r.logger.Warn("site-visit: log append failed", "project", folder, "error", err)
		}
		// Roll back the dedup entry so a transient failure can retry next fix.
		r.mu.Lock()
		delete(r.seen, key)
		r.mu.Unlock()
		return
	}
	r.persist()
	if r.logger != nil {
		r.logger.Info("site-visit recorded", "project", folder, "site", matchedKey)
	}
}

// persist serializes state writes and always snapshots the LATEST seen map
// right before writing, under persistMu — so two concurrent Record calls can't
// let an older snapshot overwrite a newer one and drop dedup keys.
func (r *SiteVisitRecorder) persist() {
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	r.mu.Lock()
	state := r.snapshotLocked()
	r.mu.Unlock()
	if err := r.saveState(state); err != nil && r.logger != nil {
		r.logger.Warn("site-visit: state persist failed", "error", err)
	}
}

// sanitizeLogText collapses newlines, control chars, and runs of whitespace to
// single spaces so untrusted place text stays one inert markdown token.
func sanitizeLogText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r < 0x20 {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// --- state (dedup) persistence ---

type siteVisitState struct {
	Version int              `json:"version"`
	Seen    map[string]int64 `json:"seen"`
}

func (r *SiteVisitRecorder) ensureLoadedLocked() {
	if r.loaded {
		return
	}
	r.loaded = true
	data, err := os.ReadFile(r.statePath)
	if err != nil {
		return
	}
	var st siteVisitState
	if json.Unmarshal(data, &st) == nil && st.Seen != nil {
		r.seen = st.Seen
	}
}

func (r *SiteVisitRecorder) pruneLocked() {
	if len(r.seen) <= siteVisitStateCap {
		return
	}
	// Drop the oldest entries by timestamp.
	type kv struct {
		k string
		v int64
	}
	all := make([]kv, 0, len(r.seen))
	for k, v := range r.seen {
		all = append(all, kv{k, v})
	}
	// Partial selection: keep the newest siteVisitStateCap.
	for len(r.seen) > siteVisitStateCap {
		oldestK, oldestV := "", int64(1<<62)
		for _, e := range all {
			if _, ok := r.seen[e.k]; ok && e.v < oldestV {
				oldestK, oldestV = e.k, e.v
			}
		}
		if oldestK == "" {
			break
		}
		delete(r.seen, oldestK)
	}
}

func (r *SiteVisitRecorder) snapshotLocked() siteVisitState {
	cp := make(map[string]int64, len(r.seen))
	for k, v := range r.seen {
		cp[k] = v
	}
	return siteVisitState{Version: 1, Seen: cp}
}

func (r *SiteVisitRecorder) saveState(state siteVisitState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(r.statePath, data, &atomicfile.Options{Perm: 0o600})
}
