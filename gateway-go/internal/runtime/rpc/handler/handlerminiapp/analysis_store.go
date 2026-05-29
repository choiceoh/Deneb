// analysis_store.go — per-message JSON cache for Mini App mail analyses.
//
// Mail bodies are immutable, so a successful "🔍 분석" result is safe to
// keep forever for that message ID. When the operator reopens the same
// email and taps analyze, the handler returns the stored copy instantly
// (no 30s-4min LLM round trip, no duplicate spend).
//
// On-disk format: one JSON file per message at <dir>/<msgID>.json,
// written atomically via pkg/atomicfile. No in-memory index — lookups
// are per-tap and payloads are small.
//
// Prompt-version handshake: every record stores the prompt version it
// was generated under. On read, a mismatch is treated as a cache miss
// so a prompt change automatically forces fresh runs. We don't delete
// the stale file — a rollback can resurrect it.

package handlerminiapp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// AnalysisPromptVersion bumps when the gmailpoll analysis prompt
// changes in a way that should invalidate cached results.
const AnalysisPromptVersion = "v1"

// analysisRecord is the on-disk shape of one cached analysis.
type analysisRecord struct {
	MsgID           string    `json:"msgID"`
	Subject         string    `json:"subject,omitempty"`
	From            string    `json:"from,omitempty"`
	Date            string    `json:"date,omitempty"`
	Analysis        string    `json:"analysis"`
	RelatedProjects []string  `json:"relatedProjects,omitempty"` // wiki paths of related project pages
	DurationMs      int64     `json:"durationMs"`
	PromptVersion   string    `json:"promptVersion"`
	CreatedAt       time.Time `json:"createdAt"`
}

// AnalysisStore is a per-message JSON cache rooted at a directory.
// A zero value (or nil pointer) is a valid no-op store — handlers can
// keep their write/load calls unconditional.
type AnalysisStore struct {
	dir string
}

// NewAnalysisStore returns a store rooted at dir. An empty dir disables
// the store; load returns (nil, nil) and save is a no-op. This keeps
// wiring in method_registry.go straightforward when the data dir isn't
// available yet (e.g., very early startup or tests).
func NewAnalysisStore(dir string) *AnalysisStore {
	return &AnalysisStore{dir: dir}
}

// load returns the cached record for msgID, or (nil, nil) on a miss.
// A record stamped with a different PromptVersion than the current
// AnalysisPromptVersion is reported as a miss (no error) so the caller
// transparently re-runs analysis.
func (s *AnalysisStore) load(msgID string) (*analysisRecord, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}
	path := s.pathFor(msgID)
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var rec analysisRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, err
	}
	if rec.PromptVersion != AnalysisPromptVersion {
		return nil, nil
	}
	return &rec, nil
}

// save persists the record. Returns an error so the caller can log it;
// the handler treats persistence failure as non-fatal so a working LLM
// result is never surfaced as a failure.
func (s *AnalysisStore) save(rec *analysisRecord) error {
	if s == nil || s.dir == "" {
		return nil
	}
	path := s.pathFor(rec.MsgID)
	if path == "" {
		return errors.New("analysis store: invalid msgID")
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, data, nil)
}

// CachedAnalysis is the public payload for storing an analysis produced
// outside the analyze handler (e.g., the autonomous Gmail poller) so it
// lands in the same cache the Mini App reads. PromptVersion is stamped
// automatically so a poller-written record is served by analyze/
// analysis_cached exactly like a manually-run one.
type CachedAnalysis struct {
	MsgID           string
	Subject         string
	From            string
	Date            string
	Analysis        string
	RelatedProjects []string
	DurationMs      int64
	CreatedAt       time.Time
}

// SaveAnalysis stores a CachedAnalysis under the current prompt version.
// Best-effort: callers log the error but don't fail the poll cycle on it.
func (s *AnalysisStore) SaveAnalysis(in CachedAnalysis) error {
	return s.save(&analysisRecord{
		MsgID:           in.MsgID,
		Subject:         in.Subject,
		From:            in.From,
		Date:            in.Date,
		Analysis:        in.Analysis,
		RelatedProjects: in.RelatedProjects,
		DurationMs:      in.DurationMs,
		PromptVersion:   AnalysisPromptVersion,
		CreatedAt:       in.CreatedAt,
	})
}

// pathFor returns the on-disk path for msgID. Returns "" if the ID
// contains characters that could escape the cache dir — Gmail IDs are
// normally [a-zA-Z0-9_-] only, so anything else is treated as hostile
// and refused.
func (s *AnalysisStore) pathFor(msgID string) string {
	if msgID == "" {
		return ""
	}
	if strings.ContainsAny(msgID, `/\.`) || strings.ContainsRune(msgID, 0) {
		return ""
	}
	return filepath.Join(s.dir, msgID+".json")
}
