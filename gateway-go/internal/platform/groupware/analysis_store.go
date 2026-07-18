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
// v2: price-memory loop — 과거 단가·경비 이력 주입 + 단가 비교 섹션.
// v3: selected attachments (계약/견적/…) downloaded+OCR'd into the analysis prompt.
// v4: PROJECT_FILE trailer — agent judges whether to file to project wiki.
// v5: precedent recall — 과거 유사 결재 주입 + 전례 대조 섹션.
const ApprovalAnalysisPromptVersion = "v5"

// ApprovalAnalysisRecord is the on-disk shape of one cached 전자결재 analysis.
type ApprovalAnalysisRecord struct {
	DocID      string `json:"docId"`
	Title      string `json:"title,omitempty"`
	Drafter    string `json:"drafter,omitempty"`
	Date       string `json:"date,omitempty"`
	Analysis   string `json:"analysis"`
	Importance string `json:"importance,omitempty"`
	// ProjectFile is the model's judgment that this approval is worth appending
	// to the matched project's 로그.md / 현재 상태 (orthogonal to Importance).
	ProjectFile   bool      `json:"projectFile,omitempty"`
	DurationMs    int64     `json:"durationMs"`
	PromptVersion string    `json:"promptVersion"`
	CreatedAt     time.Time `json:"createdAt"`
}

// ApprovalAnalysisGistLine extracts the 요지 line from an analysis body — the
// one-line gist feed cards and letters lead with. Empty when absent.
func ApprovalAnalysisGistLine(analysis string) string {
	for _, line := range strings.Split(analysis, "\n") {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "**요지**") && !strings.HasPrefix(t, "요지") &&
			!strings.Contains(strings.ToLower(t), "**요지**") {
			continue
		}
		t = strings.TrimSpace(strings.TrimPrefix(t, "**요지**"))
		t = strings.TrimSpace(strings.TrimPrefix(t, "요지"))
		t = strings.TrimSpace(strings.TrimLeft(t, "：:.—- "))
		if t != "" {
			return t
		}
	}
	return ""
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
