package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/pathutil"
)

// defaultListCap bounds a List/Search result when the caller passes no limit.
const defaultListCap = 2000

// ErrPathEscape is returned when a virtual path would resolve outside the root.
// It should be unreachable in practice (vpath already clamps), but resolve()
// re-checks as defense-in-depth — local-FS backends are the one place a path
// bug becomes an arbitrary-read/write, so the guard is doubled.
var ErrPathEscape = errors.New("filestore: path escapes store root")

// ErrContentSearchPartial reports that a complete content search could not read
// or extract one or more entries. Callers may preserve any hits, but must not
// present the result as exhaustive.
var ErrContentSearchPartial = errors.New("filestore: content search partial")

// LocalStore is a Store backed by a single local-filesystem root directory.
// Every virtual path is clamped inside root; "../" and absolute re-anchoring
// can never climb above it.
type LocalStore struct {
	root string // absolute, cleaned filesystem path
}

// compile-time assertion that LocalStore satisfies Store.
var _ Store = (*LocalStore)(nil)

// NewLocalStore opens (creating if needed) a store rooted at dir. dir may
// contain ${ENV} references.
func NewLocalStore(dir string) (*LocalStore, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("filestore: root dir required")
	}
	abs, err := filepath.Abs(os.ExpandEnv(dir))
	if err != nil {
		return nil, fmt.Errorf("filestore: resolve root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("filestore: mkdir root: %w", err)
	}
	return &LocalStore{root: abs}, nil
}

// DefaultDir returns the configured store root: $DENEB_FILES_DIR, else
// ~/.deneb/files. This single indirection is what makes the store
// location-independent — pointing it at an srv4 mount (or running the gateway
// on srv4) changes only this path, never the code.
func DefaultDir() string {
	if d := strings.TrimSpace(os.Getenv("DENEB_FILES_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".deneb", "files")
	}
	return filepath.Join(home, ".deneb", "files")
}

// DefaultLocalStore opens the store at DefaultDir().
func DefaultLocalStore() (*LocalStore, error) {
	return NewLocalStore(DefaultDir())
}

// Root returns the absolute filesystem root (for the download route and tests).
func (s *LocalStore) Root() string { return s.root }

// vpath normalizes a virtual path to a "/"-rooted, cleaned, forward-slash path.
// "" and "/" both become "/". path.Clean on a rooted path resolves ".." away
// and can never climb above "/", so the result is always within the store
// (e.g. "/../../etc/passwd" → "/etc/passwd", which maps under root).
func vpath(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

// resolve maps a virtual path to an absolute filesystem path inside root and
// returns both the absolute path and the normalized virtual path. JoinUnder
// (filepath.Rel + ".." rejection) is the CodeQL-recognized containment check;
// vpath already clamps ".." before the join.
func (s *LocalStore) resolve(virt string) (abs, clean string, err error) {
	clean = vpath(virt)
	if clean == "/" {
		return s.root, clean, nil
	}
	rel := strings.TrimPrefix(clean, "/")
	abs, err = pathutil.JoinUnder(s.root, filepath.FromSlash(rel))
	if err != nil {
		return "", "", ErrPathEscape
	}
	if err := s.rejectSymlinkComponents(abs); err != nil {
		return "", "", err
	}
	return abs, clean, nil
}

func (s *LocalStore) rejectSymlinkComponents(abs string) error {
	rel, err := filepath.Rel(s.root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrPathEscape
	}
	if rel == "." {
		return nil
	}
	current := s.root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrPathEscape
		}
	}
	return nil
}

// entryFor builds an Entry from a normalized virtual path and FileInfo.
func entryFor(virt string, fi fs.FileInfo) Entry {
	if fi.IsDir() {
		return Entry{
			Tag:         "folder",
			Name:        fi.Name(),
			PathDisplay: virt,
			PathLower:   strings.ToLower(virt),
			ID:          virt,
		}
	}
	return Entry{
		Tag:            "file",
		Name:           fi.Name(),
		PathDisplay:    virt,
		PathLower:      strings.ToLower(virt),
		ID:             virt,
		Size:           fi.Size(),
		ServerModified: fi.ModTime().UTC().Format(time.RFC3339),
	}
}

// childVPath joins a normalized parent virtual dir with a child name.
func childVPath(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

// List returns entries under dir. Folders sort before files, then by name.
func (s *LocalStore) List(ctx context.Context, dir string, recursive bool, limit int) ([]Entry, error) {
	if limit <= 0 || limit > defaultListCap {
		limit = defaultListCap
	}
	return s.list(ctx, dir, recursive, limit)
}

// ListAll returns every entry under dir without the user-facing List cap. It is
// intended for bounded background indexing and request paths that carry their
// own context deadline; ordinary browsing must keep using List.
func (s *LocalStore) ListAll(ctx context.Context, dir string, recursive bool) ([]Entry, error) {
	return s.list(ctx, dir, recursive, 0)
}

func (s *LocalStore) list(ctx context.Context, dir string, recursive bool, limit int) ([]Entry, error) {
	abs, clean, err := s.resolve(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("filestore: not a directory: %s", clean)
	}

	var entries []Entry
	incomplete := false
	if recursive {
		err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if limit == 0 {
					incomplete = true
				}
				return nil //nolint:nilerr // skip unreadable entries, keep walking
			}
			if p == abs {
				return nil // skip the directory itself
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			if isInternalName(d.Name()) {
				return nil
			}
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
			rel, relErr := filepath.Rel(abs, p)
			if relErr != nil {
				if limit == 0 {
					incomplete = true
				}
				return nil //nolint:nilerr // unexpected; skip this entry
			}
			virt := path.Join(clean, filepath.ToSlash(rel))
			fi, fiErr := d.Info()
			if fiErr != nil {
				if limit == 0 {
					incomplete = true
				}
				return nil //nolint:nilerr // entry vanished mid-walk; skip
			}
			entries = append(entries, entryFor(virt, fi))
			if limit > 0 && len(entries) >= limit {
				return fs.SkipAll
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.SkipAll) {
			return entries, err
		}
	} else {
		des, derr := os.ReadDir(abs)
		if derr != nil {
			return nil, derr
		}
		for _, d := range des {
			if isInternalName(d.Name()) {
				continue
			}
			if d.Type()&fs.ModeSymlink != 0 {
				continue
			}
			if limit > 0 && len(entries) >= limit {
				break
			}
			fi, fiErr := d.Info()
			if fiErr != nil {
				if limit == 0 {
					incomplete = true
				}
				continue
			}
			entries = append(entries, entryFor(childVPath(clean, d.Name()), fi))
		}
	}
	sortEntries(entries)
	if incomplete {
		return entries, ErrContentSearchPartial
	}
	return entries, nil
}

// Search returns entries whose name contains query (case-insensitive), walking
// the whole tree up to maxResults.
func (s *LocalStore) Search(ctx context.Context, query string, maxResults int) ([]Entry, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, fmt.Errorf("filestore: 검색어가 비어 있습니다") //nolint:staticcheck // ST1005 — Korean error surfaced to user
	}
	if maxResults <= 0 || maxResults > 100 {
		maxResults = 20
	}
	var out []Entry
	err := filepath.WalkDir(s.root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // skip unreadable, keep walking
		}
		if p == s.root {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if isInternalName(d.Name()) {
			return nil
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if strings.Contains(strings.ToLower(d.Name()), q) {
			rel, relErr := filepath.Rel(s.root, p)
			if relErr == nil {
				if fi, fiErr := d.Info(); fiErr == nil {
					out = append(out, entryFor("/"+filepath.ToSlash(rel), fi))
				}
			}
		}
		if len(out) >= maxResults {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return out, err
	}
	sortEntries(out)
	return out, nil
}

// MaxContentExtractBytes bounds the per-file payload SearchContent will run
// through extractFn. Files larger than this match on name only — extracting a
// huge PDF/spreadsheet for a substring scan would dominate latency for little
// gain, so the cap keeps the walk bounded.
const MaxContentExtractBytes = 5 << 20 // 5 MiB

// SearchContent walks the tree and returns files whose name OR extracted text
// contains query (case-insensitive). It reuses List(recursive) to enumerate and
// Get to read each file, so the same path clamping and internal-file filtering
// apply. Folders are skipped (no content to scan). Files over
// maxContentExtractBytes — or that fail to read — match on name only. A nil
// extractFn (or text it returns empty for) reduces to name-only matching, so
// SearchContent is a strict superset of Search.
func (s *LocalStore) SearchContent(ctx context.Context, query string, maxResults int, extractFn func(ctx context.Context, data []byte, name string) string) ([]Entry, error) {
	return s.searchContent(ctx, query, maxResults, extractFn, false)
}

// SearchContentComplete searches the entire local store rather than the first
// defaultListCap entries. Callers must provide a bounded context because text
// extraction can be expensive on a large corpus. Rows collected before a
// deadline are returned together with the context error.
func (s *LocalStore) SearchContentComplete(ctx context.Context, query string, maxResults int, extractFn func(ctx context.Context, data []byte, name string) string) ([]Entry, error) {
	return s.searchContent(ctx, query, maxResults, extractFn, true)
}

func (s *LocalStore) searchContent(ctx context.Context, query string, maxResults int, extractFn func(ctx context.Context, data []byte, name string) string, complete bool) ([]Entry, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, fmt.Errorf("filestore: 검색어가 비어 있습니다") //nolint:staticcheck // ST1005 — Korean error surfaced to user
	}
	if maxResults <= 0 || maxResults > 100 {
		maxResults = 20
	}
	// Ordinary callers retain the browsing cap. Evidence search opts into a full
	// walk under its own deadline so the 2,001st file cannot be silently omitted.
	var all []Entry
	var err error
	if complete {
		all, err = s.ListAll(ctx, "/", true)
	} else {
		all, err = s.List(ctx, "/", true, defaultListCap)
	}
	incomplete := errors.Is(err, ErrContentSearchPartial)
	if err != nil && !incomplete {
		return nil, err
	}
	var out []Entry
	// Exact filename matches are the strongest lexical signal. Collect them in a
	// cheap first pass so earlier content-only rows cannot consume the result cap
	// before a later exact filename is even inspected.
	for _, e := range all {
		if cerr := ctx.Err(); cerr != nil {
			return out, cerr
		}
		if e.IsFolder() {
			continue // no content to scan
		}
		if strings.Contains(strings.ToLower(e.Name), q) {
			out = append(out, e)
			if len(out) >= maxResults {
				return finishContentSearch(out, incomplete)
			}
		}
	}

	// Fill only the remaining slots with content matches. Filename matches are
	// skipped because they were already admitted in the first pass.
	for _, e := range all {
		if cerr := ctx.Err(); cerr != nil {
			return out, cerr
		}
		if e.IsFolder() || strings.Contains(strings.ToLower(e.Name), q) {
			continue
		}
		// Content match: read + extract, skipping oversized or unreadable files
		// (they fall back to the already-checked name match, i.e. no match here).
		if extractFn == nil || e.Size > MaxContentExtractBytes {
			if complete && extractFn != nil && e.Size > MaxContentExtractBytes {
				incomplete = true
			}
			continue
		}
		data, gerr := s.readContentFile(ctx, e.PathDisplay, MaxContentExtractBytes)
		if gerr != nil {
			if errors.Is(gerr, context.Canceled) || errors.Is(gerr, context.DeadlineExceeded) {
				return out, gerr
			}
			if complete {
				incomplete = true
			}
			continue // vanished/unreadable mid-walk — name didn't match, so skip
		}
		text := extractFn(ctx, data, e.Name)
		if text != "" && strings.Contains(strings.ToLower(text), q) {
			out = append(out, e)
			if len(out) >= maxResults {
				break
			}
		}
	}
	return finishContentSearch(out, incomplete)
}

func finishContentSearch(out []Entry, incomplete bool) ([]Entry, error) {
	sortEntries(out)
	if incomplete {
		return out, ErrContentSearchPartial
	}
	return out, nil
}

// readContentFile enforces the extraction cap on bytes actually read, not just
// on stale directory metadata. A file can grow or be replaced after ListAll;
// LimitReader keeps that TOCTOU race from turning into an unbounded os.ReadFile.
func (s *LocalStore) readContentFile(ctx context.Context, p string, maxBytes int64) ([]byte, error) {
	r, meta, err := s.Open(ctx, p)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if meta != nil && meta.Size > maxBytes {
		return nil, ErrContentSearchPartial
	}
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrContentSearchPartial
	}
	return data, nil
}

// Get returns the file bytes and metadata at path.
func (s *LocalStore) Get(ctx context.Context, p string) ([]byte, *Entry, error) {
	if cerr := ctx.Err(); cerr != nil {
		return nil, nil, cerr
	}
	abs, clean, err := s.resolve(p)
	if err != nil {
		return nil, nil, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, nil, err
	}
	if fi.IsDir() {
		return nil, nil, fmt.Errorf("filestore: is a directory: %s", clean)
	}
	data, err := os.ReadFile(abs) //nolint:gosec // G304 — abs is clamped inside root by resolve()
	if err != nil {
		return nil, nil, err
	}
	e := entryFor(clean, fi)
	return data, &e, nil
}

// Open returns a read-seekable handle to the file at path for streaming
// downloads. The caller must Close it.
func (s *LocalStore) Open(ctx context.Context, p string) (io.ReadSeekCloser, *Entry, error) {
	if cerr := ctx.Err(); cerr != nil {
		return nil, nil, cerr
	}
	abs, clean, err := s.resolve(p)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(abs) //nolint:gosec // G304 — abs is clamped inside root by resolve()
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if fi.IsDir() {
		_ = f.Close()
		return nil, nil, fmt.Errorf("filestore: is a directory: %s", clean)
	}
	e := entryFor(clean, fi)
	return f, &e, nil
}

// AbsPath returns the absolute filesystem path for an existing file at virt, for
// callers that stream it directly through another tool (e.g. send_file) without
// a temp copy. Errors if the path escapes root or does not exist. This is a
// LocalStore-only escape hatch (absolute paths are a local-FS concept), kept off
// the Store interface.
func (s *LocalStore) AbsPath(virt string) (string, error) {
	abs, _, err := s.resolve(virt)
	if err != nil {
		return "", err
	}
	if !pathExists(abs) {
		return "", os.ErrNotExist
	}
	return abs, nil
}

// Put writes data at path, auto-renaming on a clash when overwrite is false.
func (s *LocalStore) Put(ctx context.Context, p string, data []byte, overwrite bool) (*Entry, error) {
	if cerr := ctx.Err(); cerr != nil {
		return nil, cerr
	}
	abs, clean, err := s.resolve(p)
	if err != nil {
		return nil, err
	}
	if clean == "/" {
		return nil, fmt.Errorf("filestore: cannot write to root")
	}
	if !overwrite {
		abs, clean = s.uniqueTarget(abs, clean)
	}
	if err := writeFileAtomic(abs, data); err != nil {
		return nil, fmt.Errorf("filestore: write %s: %w", clean, err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	e := entryFor(clean, fi)
	return &e, nil
}

// Stat returns metadata for a file or folder at path.
func (s *LocalStore) Stat(ctx context.Context, p string) (*Entry, error) {
	if cerr := ctx.Err(); cerr != nil {
		return nil, cerr
	}
	abs, clean, err := s.resolve(p)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	e := entryFor(clean, fi)
	return &e, nil
}

// Delete removes a file or empty folder at path. Removing the root is rejected;
// a non-empty folder fails (os.Remove), which is intended — callers must clear
// contents first rather than recursively nuking a tree by accident.
func (s *LocalStore) Delete(ctx context.Context, p string) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	abs, clean, err := s.resolve(p)
	if err != nil {
		return err
	}
	if clean == "/" {
		return fmt.Errorf("filestore: cannot delete root")
	}
	return os.Remove(abs)
}

// Mkdir creates the folder at path (and any missing parents). An existing folder
// is returned as-is — mkdir is idempotent — but the root is rejected and a
// pre-existing *file* at the path is an error (os.MkdirAll surfaces it).
func (s *LocalStore) Mkdir(ctx context.Context, p string) (*Entry, error) {
	if cerr := ctx.Err(); cerr != nil {
		return nil, cerr
	}
	abs, clean, err := s.resolve(p)
	if err != nil {
		return nil, err
	}
	if clean == "/" {
		return nil, fmt.Errorf("filestore: cannot create root")
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("filestore: mkdir %s: %w", clean, err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	e := entryFor(clean, fi)
	return &e, nil
}

// Move renames/moves src to dst (a rename is a move within the same parent). The
// source must exist; both ends are clamped inside root. When dst already exists
// the target is auto-renamed via uniqueTarget — the same anti-clobber rule Put
// uses, so a move never silently overwrites. Missing destination parents are
// created. Moving the root, or onto the root, is rejected.
func (s *LocalStore) Move(ctx context.Context, src, dst string) (*Entry, error) {
	if cerr := ctx.Err(); cerr != nil {
		return nil, cerr
	}
	srcAbs, srcClean, err := s.resolve(src)
	if err != nil {
		return nil, err
	}
	dstAbs, dstClean, err := s.resolve(dst)
	if err != nil {
		return nil, err
	}
	if srcClean == "/" {
		return nil, fmt.Errorf("filestore: cannot move root")
	}
	if dstClean == "/" {
		return nil, fmt.Errorf("filestore: cannot move onto root")
	}
	if !pathExists(srcAbs) {
		return nil, os.ErrNotExist
	}
	// Resolving to the same path is a no-op rename; return the source as-is so a
	// "rename to the same name" doesn't autorename into "name (1)".
	if srcAbs != dstAbs {
		dstAbs, dstClean = s.uniqueTarget(dstAbs, dstClean)
	}
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		return nil, fmt.Errorf("filestore: mkdir parent of %s: %w", dstClean, err)
	}
	if err := os.Rename(srcAbs, dstAbs); err != nil {
		return nil, fmt.Errorf("filestore: move %s -> %s: %w", srcClean, dstClean, err)
	}
	fi, err := os.Stat(dstAbs)
	if err != nil {
		return nil, err
	}
	e := entryFor(dstClean, fi)
	return &e, nil
}

// uniqueTarget returns the first non-existing "name (n).ext" variant of abs
// (and the matching virtual path) when abs already exists, mirroring Dropbox's
// add-mode autorename. Falls back to the original target after a sane cap.
func (s *LocalStore) uniqueTarget(abs, clean string) (string, string) {
	if !pathExists(abs) {
		return abs, clean
	}
	absExt := filepath.Ext(abs)
	absBase := strings.TrimSuffix(abs, absExt)
	cleanExt := path.Ext(clean)
	cleanBase := strings.TrimSuffix(clean, cleanExt)
	for i := 1; i < 10000; i++ {
		candAbs := fmt.Sprintf("%s (%d)%s", absBase, i, absExt)
		if !pathExists(candAbs) {
			return candAbs, fmt.Sprintf("%s (%d)%s", cleanBase, i, cleanExt)
		}
	}
	return abs, clean // give up; overwrite the original
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// writeFileAtomic writes data to abs via a temp file in the same directory
// followed by an atomic rename. Unlike pkg/atomicfile it leaves no ".lock"
// sidecar behind — this directory is a user-facing file listing, so a stray
// lock/temp file would show up as a bogus "file" to the agent and the native
// browser.
func writeFileAtomic(abs string, data []byte) error {
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed away
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, abs)
}

// isInternalName reports whether an entry is an in-flight temp file that must
// never surface in a listing (writeFileAtomic's tmp pattern). Renames clear it
// in the normal path; this guards the concurrent-write window.
func isInternalName(name string) bool {
	return strings.HasPrefix(name, ".tmp-")
}

// sortEntries orders folders before files, then case-insensitively by name.
func sortEntries(es []Entry) {
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].IsFolder() != es[j].IsFolder() {
			return es[i].IsFolder()
		}
		return strings.ToLower(es[i].Name) < strings.ToLower(es[j].Name)
	})
}
