package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Progressive backfill cursor for the idle skill-review lane (RSI P5 demand
// generation).
//
// The lane fires when the review loop is starved (idleReviewDue). Without a
// cursor it always re-walks the SAME newest sessions, so a quiet loop keeps
// re-mining recent work while OLDER substantial sessions never get prospected
// for skill ideas — the demand backlog (skill_opportunities.jsonl) is only ever
// fed from the most recent handful, which is exactly the "자발 발견 0회" starvation
// the drain has no producer for. The cursor walks a frontier BACKWARD through
// history: each fire prospects a bounded batch of sessions older than the
// frontier and drops the frontier past them, so the standing backlog of past
// work is progressively mined into opportunities. When nothing older remains the
// frontier wraps to the newest and the sweep restarts (new sessions get picked
// up; repeats collapse into the tracker's no-op echo window).
//
// Only the production-gated lane touches this file, so the cursor stays prod-only.

const (
	prospectCursorFileName = "skill_prospect_cursor.json"
	// prospectBatch bounds the sessions examined per fire so one tick's IO stays
	// comparable to the pre-cursor recent-N walk (WouldReview reads each
	// transcript; RunStaleReview — the LLM call — still runs at most once).
	prospectBatch = idleReviewCandidates
)

// prospectCursor persists the backward walk position.
type prospectCursor struct {
	// FrontierMs is the transcript mtime the walk has descended to; the next fire
	// looks strictly older than this. 0 means "start a fresh sweep from the newest".
	FrontierMs int64 `json:"frontierMs"`
}

// sessionMod is a reviewable session key paired with its transcript mtime (ms).
type sessionMod struct {
	key   string
	modMs int64
}

// reviewableSessionsByMtime lists every reviewable session newest-first with its
// transcript mtime. recentRealSessionKeys is the capped, key-only projection of
// the same set (filenames are session keys).
func reviewableSessionsByMtime(dir string) ([]sessionMod, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]sessionMod, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		key := strings.TrimSuffix(name, ".jsonl")
		if !idleReviewableSessionKey(key) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, sessionMod{key: key, modMs: info.ModTime().UnixMilli()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].modMs > out[j].modMs })
	return out, nil
}

// sessionsOlderThan returns the newest-first sessions strictly older than
// frontierMs. A frontier <= 0 means "no lower bound" — the whole newest-first
// list, i.e. start a fresh sweep from the top. Input must already be newest-first
// (reviewableSessionsByMtime guarantees this).
func sessionsOlderThan(sessions []sessionMod, frontierMs int64) []sessionMod {
	if frontierMs <= 0 {
		return sessions
	}
	for i, s := range sessions {
		if s.modMs < frontierMs {
			return sessions[i:]
		}
	}
	return nil
}

func prospectCursorPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".deneb", "data", prospectCursorFileName)
}

func loadProspectCursor(path string) prospectCursor {
	var c prospectCursor
	if path == "" {
		return c
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(raw, &c) // a corrupt cursor just restarts the sweep from the top
	return c
}

func saveProspectCursor(path string, c prospectCursor) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
