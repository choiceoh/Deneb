package groupware

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// ApprovalAnalysisPromptVersion bumps when the approval-analysis prompt changes
// in a way that should invalidate cached results.
const ApprovalAnalysisPromptVersion = "v1"

// ApprovalAnalysisRecord is the on-disk shape of one cached 전자결재 analysis.
type ApprovalAnalysisRecord struct {
	DocID         string    `json:"docId"`
	Title         string    `json:"title,omitempty"`
	Drafter       string    `json:"drafter,omitempty"`
	Date          string    `json:"date,omitempty"`
	Analysis      string    `json:"analysis"`
	Importance    string    `json:"importance,omitempty"`
	DurationMs    int64     `json:"durationMs"`
	PromptVersion string    `json:"promptVersion"`
	CreatedAt     time.Time `json:"createdAt"`
}

// ApprovalAnalysisStore is a per-docId JSON cache rooted at a directory.
// A zero value (or nil pointer) is a valid no-op store.
type ApprovalAnalysisStore struct {
	dir string
}

// NewApprovalAnalysisStore returns a store rooted at dir. Empty dir disables it.
func NewApprovalAnalysisStore(dir string) *ApprovalAnalysisStore {
	return &ApprovalAnalysisStore{dir: dir}
}

// Load returns the cached record for docID, or (nil, nil) on a miss / version skew.
func (s *ApprovalAnalysisStore) Load(docID string) (*ApprovalAnalysisRecord, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}
	path := s.pathFor(docID)
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
	var rec ApprovalAnalysisRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, err
	}
	if rec.PromptVersion != ApprovalAnalysisPromptVersion {
		return nil, nil
	}
	return &rec, nil
}

// Save persists the record. Best-effort for callers — a working LLM result
// must not fail the RPC solely because disk write blipped.
func (s *ApprovalAnalysisStore) Save(rec *ApprovalAnalysisRecord) error {
	if s == nil || s.dir == "" || rec == nil {
		return nil
	}
	path := s.pathFor(rec.DocID)
	if path == "" {
		return errors.New("approval analysis store: invalid docId")
	}
	if rec.PromptVersion == "" {
		rec.PromptVersion = ApprovalAnalysisPromptVersion
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, data, nil)
}

func (s *ApprovalAnalysisStore) pathFor(docID string) string {
	name := sanitizeApprovalCacheFilename(docID)
	if name == "" {
		return ""
	}
	return filepath.Join(s.dir, name+".json")
}

func sanitizeApprovalCacheFilename(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 || r < 0x20 {
			return '_'
		}
		return r
	}, id)
	if len(safe) > 200 {
		sum := sha256.Sum256([]byte(id))
		return hex.EncodeToString(sum[:])
	}
	return safe
}
