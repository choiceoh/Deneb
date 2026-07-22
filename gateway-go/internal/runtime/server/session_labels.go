// session_labels.go — durable conversation titles across gateway restarts.
//
// The session manager is pure in-memory and restarts are FREQUENT (auto-deploy
// hot-swaps the binary minutes after every landing). restoreAndWakeSessions
// rebuilds sessions by scanning the transcript dir, which dropped every Label —
// so the drawer's auto-generated conversation titles (chat.autoTitleSessionAsync,
// set-once) reverted to raw keys ("대화 · e6623080") on the next deploy, and old
// idle conversations never got re-titled.
//
// Two pieces close the loop:
//  1. A sidecar label store (~/.deneb/session-labels.json) — a periodic sweep
//     snapshots {sessionKey → Label} for restorable sessions (write only on
//     change) and a final flush runs on shutdown; the restore path re-applies
//     stored labels.
//  2. A one-shot backfill — restored sessions still missing a label get one
//     derived from their transcript's first exchange via the same titler the
//     live path uses (chat.GenerateSessionTitle, tiny role with heuristic
//     fallback). After the first successful backfill the store carries every
//     label, so later restarts make zero model calls.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

const (
	sessionLabelSweepInterval = time.Minute
	// Backfill is bounded to the newest conversations: enough to clean the
	// drawer's visible history without an unbounded startup scan.
	sessionLabelBackfillMax = 40
	sessionLabelBackfillGap = 200 * time.Millisecond
	// Per-title budget for the backfill titler (matches the live path's bound).
	sessionLabelTitleTimeout = 15 * time.Second
	// Head-scan budget when extracting a transcript's first exchange.
	transcriptHeadMaxLines = 80
	transcriptHeadMaxBytes = 512 * 1024
)

func sessionLabelStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".deneb", "session-labels.json"), nil
}

// loadSessionLabels reads the sidecar store; a missing or corrupt file degrades
// to an empty map (labels regenerate via backfill — never fail startup on this).
func loadSessionLabels(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]string{}
	}
	return out
}

// saveSessionLabels writes the store atomically (tmp + rename).
func saveSessionLabels(path string, labels map[string]string) error {
	data, err := json.MarshalIndent(labels, "", " ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// snapshotSessionLabels collects the labels worth persisting: restorable
// (transcript-backed) sessions with a non-empty label.
func snapshotSessionLabels(sessions []*session.Session) map[string]string {
	out := map[string]string{}
	for _, s := range sessions {
		if s == nil || strings.TrimSpace(s.Label) == "" {
			continue
		}
		if _, ok := session.RestorableTranscriptChannel(s.Key); !ok {
			continue
		}
		out[s.Key] = s.Label
	}
	return out
}

func labelsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// --- Pinned-label registry (sidecar, parallel to the label store) ------------
//
// A user rename pins the label (session.LabelPinned) so the periodic auto-title
// can't overwrite it. The pin must survive the frequent hot-swap restarts, so we
// persist the set of pinned session keys alongside the labels. Kept in its own
// file rather than folded into the label store so the proven label format (and
// its tests) is untouched; the two are written by the same sweep, and a rare
// cross-file skew only risks one extra re-title, never a lost label.

func sessionPinsStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".deneb", "session-pins.json"), nil
}

// loadSessionPins reads the pinned-key set; a missing or corrupt file degrades to
// an empty set (a lost pin only means a session becomes re-titleable again).
func loadSessionPins(path string) map[string]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]bool{}
	}
	var keys []string
	if err := json.Unmarshal(data, &keys); err != nil {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out
}

// saveSessionPins writes the pinned-key set atomically (tmp + rename), sorted for
// a stable on-disk diff.
func saveSessionPins(path string, pins map[string]bool) error {
	keys := make([]string, 0, len(pins))
	for k := range pins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	data, err := json.MarshalIndent(keys, "", " ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// snapshotSessionPins collects the keys of restorable sessions whose label is
// pinned.
func snapshotSessionPins(sessions []*session.Session) map[string]bool {
	out := map[string]bool{}
	for _, s := range sessions {
		if s == nil || !s.LabelPinned {
			continue
		}
		if _, ok := session.RestorableTranscriptChannel(s.Key); !ok {
			continue
		}
		out[s.Key] = true
	}
	return out
}

func pinsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// startSessionLabelPersistence runs the sweep loop until shutdown, with a final
// flush so a label minted seconds before a hot-swap still survives it.
func (s *Server) startSessionLabelPersistence() {
	path, err := sessionLabelStorePath()
	if err != nil {
		s.logger.Warn("session labels: cannot resolve store path", "error", err)
		return
	}
	pinsPath, pinsPathErr := sessionPinsStorePath()
	safego.GoWithSlog(s.logger, "session-label-persist", func() {
		ctx := s.ShutdownCtx()
		last := loadSessionLabels(path)
		var lastPins map[string]bool
		if pinsPathErr == nil {
			lastPins = loadSessionPins(pinsPath)
		}
		flush := func() {
			live := s.sessions.List()
			snap := snapshotSessionLabels(live)
			// Merge over the stored map: a session evicted from memory keeps its
			// stored title for the next restore instead of being dropped.
			merged := make(map[string]string, len(last)+len(snap))
			for k, v := range last {
				merged[k] = v
			}
			for k, v := range snap {
				merged[k] = v
			}
			if !labelsEqual(merged, last) {
				if err := saveSessionLabels(path, merged); err != nil {
					s.logger.Warn("session labels: persist failed", "error", err)
				} else {
					last = merged
				}
			}
			// Pins ride the same sweep. Merge over the stored set so a pin whose
			// session is evicted from memory survives to the next restore.
			if pinsPathErr != nil {
				return
			}
			pinSnap := snapshotSessionPins(live)
			mergedPins := make(map[string]bool, len(lastPins)+len(pinSnap))
			for k := range lastPins {
				mergedPins[k] = true
			}
			for k := range pinSnap {
				mergedPins[k] = true
			}
			if pinsEqual(mergedPins, lastPins) {
				return
			}
			if err := saveSessionPins(pinsPath, mergedPins); err != nil {
				s.logger.Warn("session pins: persist failed", "error", err)
				return
			}
			lastPins = mergedPins
		}
		ticker := time.NewTicker(sessionLabelSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				flush()
				return
			case <-ticker.C:
				flush()
			}
		}
	})
}

// backfillSessionTitles titles restored sessions that have no stored label yet,
// newest first, using each transcript's first exchange. Sequential and paced —
// this is cleanup, not a startup race; the tiny role is local and the heuristic
// fallback keeps it functional with no model at all.
func (s *Server) backfillSessionTitles(transcriptDir string, keys []string) {
	if len(keys) == 0 {
		return
	}
	if len(keys) > sessionLabelBackfillMax {
		keys = keys[:sessionLabelBackfillMax]
	}
	safego.GoWithSlog(s.logger, "session-label-backfill", func() {
		ctx := s.ShutdownCtx()
		titled := 0
		for _, key := range keys {
			if ctx.Err() != nil {
				return
			}
			// Re-check set-once: a live turn may have titled it meanwhile.
			if sess := s.sessions.Get(key); sess == nil || strings.TrimSpace(sess.Label) != "" {
				continue
			}
			userMsg, reply := readTranscriptFirstExchange(filepath.Join(transcriptDir, key+".jsonl"))
			if strings.TrimSpace(userMsg) == "" {
				continue
			}
			callCtx, cancel := context.WithTimeout(ctx, sessionLabelTitleTimeout)
			title := chat.GenerateSessionTitle(callCtx, userMsg, reply)
			cancel()
			if title == "" {
				continue
			}
			s.sessions.Patch(key, session.PatchFields{Label: &title})
			titled++
			time.Sleep(sessionLabelBackfillGap)
		}
		if titled > 0 {
			s.logger.Info("session labels: backfilled conversation titles", "count", titled)
		}
	})
}

// transcriptLine is the minimal shape of a transcript JSONL record needed to
// pull the first exchange: user content is a plain string, assistant content is
// a block array whose text blocks carry the reply.
type transcriptLine struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// readTranscriptFirstExchange head-scans a transcript for the first user text
// and the first assistant text after it. Best-effort: any parse hiccup just
// yields what was collected so far.
func readTranscriptFirstExchange(path string) (userMsg, reply string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), transcriptHeadMaxBytes)
	for lines := 0; sc.Scan() && lines < transcriptHeadMaxLines; lines++ {
		var line transcriptLine
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			continue
		}
		switch line.Role {
		case "user":
			if userMsg != "" {
				continue
			}
			var text string
			if err := json.Unmarshal(line.Content, &text); err == nil {
				userMsg = stripTimestampPrefix(text)
			}
		case "assistant":
			if userMsg == "" || reply != "" {
				continue
			}
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(line.Content, &blocks); err != nil {
				continue
			}
			for _, b := range blocks {
				if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
					reply = b.Text
					break
				}
			}
			if reply != "" {
				return userMsg, reply
			}
		}
	}
	return userMsg, reply
}

// stripTimestampPrefix drops the "[2026-06-13T23:33:24+09:00] " prefix the
// pipeline stamps onto persisted user messages — it is noise for a titler.
func stripTimestampPrefix(s string) string {
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(trimmed, "[") {
		if end := strings.Index(trimmed, "] "); end > 0 && end < 48 {
			return strings.TrimSpace(trimmed[end+2:])
		}
	}
	return trimmed
}

// sortSessionKeysNewestFirst orders backfill candidates by transcript mtime so
// the drawer's visible top gets titles first under the backfill cap.
func sortSessionKeysNewestFirst(transcriptDir string, keys []string) []string {
	type keyed struct {
		key string
		mod int64
	}
	rows := make([]keyed, 0, len(keys))
	for _, k := range keys {
		var mod int64
		if info, err := os.Stat(filepath.Join(transcriptDir, k+".jsonl")); err == nil {
			mod = info.ModTime().UnixMilli()
		}
		rows = append(rows, keyed{key: k, mod: mod})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].mod > rows[j].mod })
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.key
	}
	return out
}
