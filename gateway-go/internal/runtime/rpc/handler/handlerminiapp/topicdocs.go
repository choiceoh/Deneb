// topicdocs.go — miniapp.topicdocs.* RPC handlers (single-topic editor).
//
// "Topic background" is the per-topic knowledge file at
// <workspace>/topics/<key>.md that gets injected into the system prompt's
// Static (cached) block for that topic's sessions (see config.TopicsConfig and
// prompt.LoadTopicKnowledge). These RPCs let the operator read/edit the
// *current* topic's doc from the native Settings surface instead of SSHing to
// the workstation.
//
// History: PR #2179 deleted the old multi-file browser (list_files/read_file/
// write_file) because the topics directory holds exactly one live file in the
// single-topic model ({"0":"업무"}), so a file browser was overkill and its
// "new document" action created files no session would ever inject. This is the
// redesign: not a browser, but a single-document editor scoped to the one
// current topic key. The client never names a file — the gateway resolves the
// key (CurrentKey) and the path itself, closing the path-injection hole #2179
// intentionally shut.
//
// These are PLAIN .md files (no YAML frontmatter), unrelated to the wiki store:
// raw file I/O directly under the topics dir. Routing them through wiki.Store
// would inject a frontmatter block and corrupt the file. We borrow only the
// handler *shape* (lazy-factory Deps, requireAuth, RespondOK), not its storage.

package handlerminiapp

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// maxTopicDocBytes caps a topic-doc write. It is pinned to
// prompt.MaxTopicKnowledgeChars (the per-topic Static-block injection cap) so
// the editor can never persist content the prompt loader would silently
// truncate on injection — what you save is what the model sees. (The old #2179
// handler used 512KB, which let an operator save text that never fully
// injected.)
const maxTopicDocBytes = prompt.MaxTopicKnowledgeChars

// TopicDocsDeps wires the single-topic editor.
//
//   - TopicsDir resolves "<workspace>/<topics.dir>" lazily (per call) so a
//     config change takes effect without a restart. A "" / error result makes
//     the handler respond UNAVAILABLE.
//   - CurrentKey resolves the current topic key (e.g. "업무" for the native
//     home topic "0"). The client never sends a file name; the key fully
//     determines the file (<dir>/<key>.md). A "" key makes the handler respond
//     UNAVAILABLE (topics unconfigured).
//
// Future seam: a list_keys method + a Key param could expose multiple topics if
// the map ever grows past one entry — intentionally omitted while {"0":"업무"}
// is the only live mapping.
type TopicDocsDeps struct {
	TopicsDir  func() (string, error)
	CurrentKey func() string
	// ApplyNow, when non-nil, is invoked after a write with applyNow=true to
	// drop the session-frozen topic snapshots so the edit lands this session
	// (the RPC analog of a slash "--now"). nil leaves writes deferred-only
	// (next-session reflection), which is the safe default for the Static
	// prompt cache.
	ApplyNow func()
}

// TopicDocOut is the current topic document returned by read_current.
//
//deneb:wire
type TopicDocOut struct {
	Key      string `json:"key"`      // current topic key (e.g. "업무")
	Name     string `json:"name"`     // resolved file name (e.g. "업무.md")
	Content  string `json:"content"`  // file body ("" when the file does not exist yet)
	Size     int64  `json:"size"`     // body size in bytes
	Modified string `json:"modified"` // RFC3339 mod time ("" when the file does not exist yet)
}

// TopicDocWriteOut is the write_current result.
//
//deneb:wire
type TopicDocWriteOut struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Applied  bool   `json:"applied"` // true when applyNow cleared the session snapshots this turn
}

// TopicDocsMethods returns the miniapp.topicdocs.* handler map. Returns nil when
// either factory is unwired so method_registry can register conditionally
// (topics unconfigured → no editor surface).
func TopicDocsMethods(deps TopicDocsDeps) map[string]rpcutil.HandlerFunc {
	if deps.TopicsDir == nil || deps.CurrentKey == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.topicdocs.read_current":  topicDocsReadCurrent(deps),
		"miniapp.topicdocs.write_current": topicDocsWriteCurrent(deps),
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

// resolveCurrentKey resolves the current topic key, rejecting empty/unsafe
// values. The key is config-owned (never client-supplied), but a path-traversal
// guard is kept defensively — the key alone determines the file, so a malformed
// map entry must not be able to escape the topics dir.
func resolveCurrentKey(deps TopicDocsDeps) (string, error) {
	key := strings.TrimSpace(deps.CurrentKey())
	if key == "" {
		return "", errors.New("no current topic configured")
	}
	if filepath.Base(key) != key {
		return "", errors.New("topic key must not contain a path")
	}
	if strings.ContainsAny(key, `/\`) || strings.Contains(key, "..") {
		return "", errors.New("topic key must not contain slashes or ..")
	}
	if strings.HasPrefix(key, ".") {
		return "", errors.New("topic key must not start with a dot")
	}
	if strings.ContainsRune(key, 0) {
		return "", errors.New("topic key must not contain control characters")
	}
	if utf8.RuneCountInString(key) > 128 {
		return "", errors.New("topic key is too long")
	}
	return key, nil
}

// topicDocsReadCurrent reads <dir>/<key>.md for the current topic key. A missing
// file yields an empty document (NOT NotFound) so the native editor opens blank
// rather than erroring — the file is created on first write.
func topicDocsReadCurrent(deps TopicDocsDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		dir, err := resolveTopicsDir(deps)
		if err != nil {
			return rpcerr.WrapUnavailable("topics dir unavailable", err).Response(req.ID)
		}
		key, err := resolveCurrentKey(deps)
		if err != nil {
			return rpcerr.WrapUnavailable("current topic unavailable", err).Response(req.ID)
		}
		name := key + ".md"
		full := filepath.Join(dir, name)

		out := TopicDocOut{Key: key, Name: name}
		data, err := os.ReadFile(full)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// Empty editor — the doc is created on first write.
				return rpcutil.RespondOK(req.ID, out)
			}
			return rpcerr.WrapUnavailable("topic file read failed", err).Response(req.ID)
		}
		out.Content = string(data)
		out.Size = int64(len(data))
		if info, statErr := os.Stat(full); statErr == nil {
			out.Size = info.Size()
			out.Modified = info.ModTime().Format(time.RFC3339)
		}
		return rpcutil.RespondOK(req.ID, out)
	}
}

// topicDocsWriteCurrent upserts <dir>/<key>.md for the current topic key. The
// client supplies only content (+ applyNow); the file name is always derived
// from the gateway-resolved key, never from the request. The parent dir is
// auto-created by atomicfile.WriteFile.
func topicDocsWriteCurrent(deps TopicDocsDeps) rpcutil.HandlerFunc {
	type params struct {
		Content  string `json:"content"`
		ApplyNow bool   `json:"applyNow,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		if strings.TrimSpace(p.Content) == "" {
			return rpcerr.InvalidRequest("topic content cannot be empty").Response(req.ID)
		}
		if len(p.Content) > maxTopicDocBytes {
			return rpcerr.ValidationFailed("topic content exceeds the injection cap").Response(req.ID)
		}
		dir, err := resolveTopicsDir(deps)
		if err != nil {
			return rpcerr.WrapUnavailable("topics dir unavailable", err).Response(req.ID)
		}
		key, err := resolveCurrentKey(deps)
		if err != nil {
			return rpcerr.WrapUnavailable("current topic unavailable", err).Response(req.ID)
		}
		name := key + ".md"
		full := filepath.Join(dir, name)

		if err := atomicfile.WriteFile(full, []byte(p.Content), nil); err != nil {
			return rpcerr.WrapUnavailable("topic file write failed", err).Response(req.ID)
		}

		out := TopicDocWriteOut{Key: key, Name: name, Size: int64(len(p.Content))}
		if info, statErr := os.Stat(full); statErr == nil {
			out.Size = info.Size()
			out.Modified = info.ModTime().Format(time.RFC3339)
		}
		// applyNow drops the per-session frozen topic snapshots so the edit
		// lands this session instead of next. Default (deferred) keeps the
		// Static prompt cache stable.
		if p.ApplyNow && deps.ApplyNow != nil {
			deps.ApplyNow()
			out.Applied = true
		}
		return rpcutil.RespondOK(req.ID, out)
	}
}
