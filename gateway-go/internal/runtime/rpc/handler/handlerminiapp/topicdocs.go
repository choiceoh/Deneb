// topicdocs.go — miniapp.topicdocs.* RPC handlers.
//
// "Topic docs" are the per-topic knowledge files under
// <workspace>/topics/*.md (e.g. coding.md, work.md) that get injected into the
// system prompt for that topic's sessions (see config.TopicsConfig). These RPCs
// let the operator list/read/write/create them from the Mini App instead of
// SSHing to the workstation.
//
// These are PLAIN .md files (no YAML frontmatter) and are unrelated to the wiki
// store: we do raw file I/O directly under the topics dir. Routing them through
// wiki.Store would inject a frontmatter block and corrupt the file. We borrow
// only memory.go's handler *shape* (lazy-factory Deps, requireAuth, RespondOK),
// not its storage. Namespace is "topicdocs", distinct from miniapp.topics.*
// which exposes the native-client topic/session contract.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const (
	maxTopicDocBytes      = 512 * 1024 // body cap on write
	maxTopicFileNameRunes = 128
)

// TopicDocsDeps wires the topic-docs handler. TopicsDir resolves
// "<workspace>/topics" lazily — resolved per call so a config change takes
// effect without a restart. A "" / error result makes the handler respond
// UNAVAILABLE.
type TopicDocsDeps struct {
	TopicsDir func() (string, error)
}

// TopicDocsMethods returns the miniapp.topicdocs.* handler map. Returns nil
// when no dir factory is wired so method_registry can register conditionally.
func TopicDocsMethods(deps TopicDocsDeps) map[string]rpcutil.HandlerFunc {
	if deps.TopicsDir == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.topicdocs.list_files": topicDocsList(deps),
		"miniapp.topicdocs.read_file":  topicDocsRead(deps),
		"miniapp.topicdocs.write_file": topicDocsWrite(deps),
	}
}

// resolveTopicsDir resolves the topics directory, normalizing a nil-error +
// empty-string result into a real error so callers always get a non-nil err.
func resolveTopicsDir(deps TopicDocsDeps) (string, error) {
	dir, err := deps.TopicsDir()
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", errors.New("workspace topics dir not resolved")
	}
	return dir, nil
}

// validateTopicFileName enforces a flat ".md" base name — no subdirectories,
// no traversal, no hidden/dot files. Stricter than validateWikiPath (memory.go),
// which allows "category/slug.md"; topic docs live in one flat directory.
func validateTopicFileName(name string) error {
	if name == "" {
		return errors.New("file name is required")
	}
	if filepath.Base(name) != name {
		return errors.New("file name must not contain a path")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return errors.New("file name must not contain slashes or ..")
	}
	if strings.HasPrefix(name, ".") {
		return errors.New("file name must not start with a dot")
	}
	if strings.ContainsRune(name, 0) {
		return errors.New("file name must not contain control characters")
	}
	if !strings.HasSuffix(name, ".md") {
		return errors.New("file name must end with .md")
	}
	if utf8.RuneCountInString(name) > maxTopicFileNameRunes {
		return errors.New("file name is too long")
	}
	return nil
}

// topicDocsList scans <topics> for *.md files (newest first). A missing
// directory is an empty list, not an error.
func topicDocsList(deps TopicDocsDeps) rpcutil.HandlerFunc {
	type file struct {
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		Modified string `json:"modified"`
	}
	type out struct {
		Files []file `json:"files"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		dir, err := resolveTopicsDir(deps)
		if err != nil {
			return rpcerr.WrapUnavailable("topics dir unavailable", err).Response(req.ID)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return rpcutil.RespondOK(req.ID, out{Files: []file{}})
			}
			return rpcerr.WrapUnavailable("topics dir read failed", err).Response(req.ID)
		}
		files := make([]file, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
				continue
			}
			f := file{Name: name}
			if info, infoErr := e.Info(); infoErr == nil {
				f.Size = info.Size()
				f.Modified = info.ModTime().Format(time.RFC3339)
			}
			files = append(files, f)
		}
		sort.Slice(files, func(i, j int) bool {
			if files[i].Modified != files[j].Modified {
				return files[i].Modified > files[j].Modified // newest first
			}
			return files[i].Name < files[j].Name
		})
		return rpcutil.RespondOK(req.ID, out{Files: files})
	}
}

// topicDocsRead returns the raw content of <topics>/<name>.
func topicDocsRead(deps TopicDocsDeps) rpcutil.HandlerFunc {
	type params struct {
		Name string `json:"name"`
	}
	type out struct {
		Name     string `json:"name"`
		Content  string `json:"content"`
		Size     int64  `json:"size"`
		Modified string `json:"modified"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return rpcerr.MissingParam("name").Response(req.ID)
		}
		if err := validateTopicFileName(name); err != nil {
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}
		dir, err := resolveTopicsDir(deps)
		if err != nil {
			return rpcerr.WrapUnavailable("topics dir unavailable", err).Response(req.ID)
		}
		full := filepath.Join(dir, name)
		data, err := os.ReadFile(full)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return rpcerr.NotFound("topic file " + rpcutil.TruncateForError(name)).Response(req.ID)
			}
			return rpcerr.WrapUnavailable("topic file read failed", err).Response(req.ID)
		}
		o := out{Name: name, Content: string(data), Size: int64(len(data))}
		if info, statErr := os.Stat(full); statErr == nil {
			o.Size = info.Size()
			o.Modified = info.ModTime().Format(time.RFC3339)
		}
		return rpcutil.RespondOK(req.ID, o)
	}
}

// topicDocsWrite upserts <topics>/<name>. create=true rejects an existing file
// (CONFLICT). The parent dir is auto-created by atomicfile.WriteFile.
func topicDocsWrite(deps TopicDocsDeps) rpcutil.HandlerFunc {
	type params struct {
		Name    string `json:"name"`
		Content string `json:"content"`
		Create  bool   `json:"create,omitempty"`
	}
	type out struct {
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		Modified string `json:"modified"`
		Created  bool   `json:"created"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return rpcerr.MissingParam("name").Response(req.ID)
		}
		if err := validateTopicFileName(name); err != nil {
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}
		if len(p.Content) > maxTopicDocBytes {
			return rpcerr.ValidationFailed("topic file exceeds 512KB").Response(req.ID)
		}
		dir, err := resolveTopicsDir(deps)
		if err != nil {
			return rpcerr.WrapUnavailable("topics dir unavailable", err).Response(req.ID)
		}
		full := filepath.Join(dir, name)

		_, statErr := os.Stat(full)
		existed := statErr == nil
		if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return rpcerr.WrapUnavailable("topic file stat failed", statErr).Response(req.ID)
		}
		if p.Create && existed {
			return rpcerr.InvalidRequest("topic file already exists: " + rpcutil.TruncateForError(name)).Response(req.ID)
		}

		if err := atomicfile.WriteFile(full, []byte(p.Content), nil); err != nil {
			return rpcerr.WrapUnavailable("topic file write failed", err).Response(req.ID)
		}

		o := out{Name: name, Size: int64(len(p.Content)), Created: !existed}
		if info, statErr := os.Stat(full); statErr == nil {
			o.Size = info.Size()
			o.Modified = info.ModTime().Format(time.RFC3339)
		}
		return rpcutil.RespondOK(req.ID, o)
	}
}
