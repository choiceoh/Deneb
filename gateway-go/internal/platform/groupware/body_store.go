package groupware

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// ApprovalBodyTTL bounds body-cache freshness: the document text itself is
// immutable once submitted, but the embedded 결재선 progress lines change as
// approvers act, so a cached body ages out quickly.
const ApprovalBodyTTL = 10 * time.Minute

// ApprovalBodyRecord is the on-disk shape of one cached 전자결재 body.
type ApprovalBodyRecord struct {
	DocID   string    `json:"docId"`
	Body    string    `json:"body"`
	SavedAt time.Time `json:"savedAt"`
}

// ApprovalBodyStore caches document bodies per docId so opening a 결재 detail
// doesn't pay the Playwright reader roundtrip every time. A nil/zero store is
// a valid no-op.
type ApprovalBodyStore struct {
	dir string
	ttl time.Duration
}

// NewApprovalBodyStore returns a store rooted at dir. Empty dir disables it.
func NewApprovalBodyStore(dir string) *ApprovalBodyStore {
	return &ApprovalBodyStore{dir: dir, ttl: ApprovalBodyTTL}
}

// Load returns the cached body for docID, or "" on miss/expiry.
func (s *ApprovalBodyStore) Load(docID string) string {
	if s == nil || s.dir == "" {
		return ""
	}
	path := s.pathFor(docID)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var rec ApprovalBodyRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return ""
	}
	ttl := s.ttl
	if ttl <= 0 {
		ttl = ApprovalBodyTTL
	}
	if time.Since(rec.SavedAt) > ttl {
		return ""
	}
	return rec.Body
}

// Save persists the body (best-effort for callers).
func (s *ApprovalBodyStore) Save(docID, body string) error {
	if s == nil || s.dir == "" || body == "" {
		return nil
	}
	path := s.pathFor(docID)
	if path == "" {
		return errors.New("approval body store: invalid docId")
	}
	data, err := json.Marshal(ApprovalBodyRecord{DocID: docID, Body: body, SavedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, data, nil)
}

func (s *ApprovalBodyStore) pathFor(docID string) string {
	name := sanitizeApprovalCacheFilename(docID)
	if name == "" {
		return ""
	}
	return filepath.Join(s.dir, name+".json")
}
