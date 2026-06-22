// project_digest_store.go — per-project JSON store for the Mini App's
// "프로젝트 진행상황" 모아보기 screen.
//
// The wiki dreamer rolls up each cycle's fresh diary/MEMORY input into one
// latest-progress digest per active project (see domain/wiki/project_digest.go)
// and hands them to a sink wired in the server. The sink upserts here: one JSON
// file per project at <dir>/<project>.json, overwriting that project's prior
// digest (the newest cycle wins). Projects that saw no activity this cycle keep
// their last digest on disk, so the screen always shows each project's most
// recent known status — not just the handful touched in the latest cycle.
//
// On-disk format mirrors analysis_store.go: one atomically-written JSON file per
// key, no in-memory index. Reads are one directory scan per screen open and
// payloads are tiny, so a flat dir is plenty.

package handlerminiapp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// projectDigestRecord is the on-disk shape of one project's latest digest.
type projectDigestRecord struct {
	Project   string    `json:"project"`
	Headline  string    `json:"headline"`
	Bullets   []string  `json:"bullets,omitempty"`
	Due       string    `json:"due,omitempty"` // YYYY-MM-DD imminent deadline, "" if none
	UpdatedAt time.Time `json:"updatedAt"`     // when this digest was generated
}

// ProjectDigestStore is a per-project JSON store rooted at a directory. Like
// AnalysisStore it is a stateless dir wrapper: a nil pointer or empty dir is a
// valid no-op (save no-ops, list returns nothing) so wiring stays unconditional.
type ProjectDigestStore struct {
	dir string
}

// NewProjectDigestStore returns a store rooted at dir. An empty dir disables the
// store. The directory is created lazily on first save (atomicfile mkdirs).
func NewProjectDigestStore(dir string) *ProjectDigestStore {
	return &ProjectDigestStore{dir: dir}
}

// ProjectDigestInput is the public payload the wiki-dream sink writes. The
// caller stamps UpdatedAt (normally time.Now()) so the store stays
// deterministic for tests.
type ProjectDigestInput struct {
	Project   string
	Headline  string
	Bullets   []string
	Due       string
	UpdatedAt time.Time
}

// SaveDigest upserts one project's digest. Best-effort from the caller's view
// (the dream cycle logs but does not fail on a persistence error).
func (s *ProjectDigestStore) SaveDigest(in ProjectDigestInput) error {
	if s == nil || s.dir == "" {
		return nil
	}
	project := strings.TrimSpace(in.Project)
	if project == "" {
		return errors.New("project digest store: empty project")
	}
	name := sanitizeCacheFilename(project)
	if name == "" {
		return errors.New("project digest store: invalid project")
	}
	rec := projectDigestRecord{
		Project:   project,
		Headline:  strings.TrimSpace(in.Headline),
		Bullets:   in.Bullets,
		Due:       strings.TrimSpace(in.Due),
		UpdatedAt: in.UpdatedAt,
	}
	data, err := json.MarshalIndent(&rec, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(filepath.Join(s.dir, name+".json"), data, nil)
}

// list returns every stored digest, newest first (UpdatedAt desc, then project
// asc for stable ties). A missing directory (no digest ever written) is not an
// error — it returns no rows.
func (s *ProjectDigestStore) list() ([]projectDigestRecord, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []projectDigestRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if rerr != nil {
			continue // skip an unreadable file rather than failing the whole list
		}
		var rec projectDigestRecord
		if json.Unmarshal(data, &rec) != nil || strings.TrimSpace(rec.Project) == "" {
			continue
		}
		out = append(out, rec)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].Project < out[j].Project
	})
	return out, nil
}
