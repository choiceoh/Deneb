package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
)

// ErrFilesRecallTimeout is returned when HybridSearch hits the files-layer
// deadline. The Router treats it as a soft degrade (empty files hits + note)
// rather than a hard recall failure.
var ErrFilesRecallTimeout = errors.New("files recall timeout")

// filesAdapter exposes the on-box file store's semantic (hybrid) index as a
// read-only knowledge backend. It is the recall counterpart to the chat files
// tool and the miniapp.files.search RPC — all three share one
// *filestore.SemanticIndex, so a file becomes recallable as soon as the
// background reindex has embedded it.
//
// Read-only by design: files arrive through upload/sync, not through the
// knowledge.record write path, so filesAdapter intentionally does NOT implement
// Writer (the wiki adapter remains the sole writable backend).
type filesAdapter struct {
	index     *filestore.SemanticIndex
	embed     filestore.Embedder
	extractFn filestore.ExtractFunc
	// readFile fetches a file's bytes + name for the Read op (the full document
	// behind a recall hit). Injected so this package never imports the tools or
	// server layer; nil disables Read (recall still works).
	readFile func(ctx context.Context, path string) (data []byte, name string, err error)
}

// FilesAdapterDeps carries the filestore wiring a files knowledge backend needs.
// Every field is optional: when index or embed is nil (no store, or the
// embedding server is down) NewFilesAdapter returns nil so the Router simply
// drops the layer and recall degrades to wiki-only — the same graceful pattern
// as NewWikiAdapter(nil).
type FilesAdapterDeps struct {
	Index     *filestore.SemanticIndex
	Embed     filestore.Embedder
	ExtractFn filestore.ExtractFunc
	// ReadFile returns a file's bytes for the Read op. Optional; when nil the
	// Read op reports that the file layer cannot fetch full documents (recall is
	// unaffected).
	ReadFile func(ctx context.Context, path string) (data []byte, name string, err error)
}

// NewFilesAdapter wraps the shared file semantic index as a knowledge backend.
// Returns nil when the index or embedder is missing so the Router can ignore an
// unconfigured/degraded backend without a nil check at the call site.
func NewFilesAdapter(deps FilesAdapterDeps) Adapter {
	if deps.Index == nil || deps.Embed == nil {
		return nil
	}
	return &filesAdapter{
		index:     deps.Index,
		embed:     deps.Embed,
		extractFn: deps.ExtractFn,
		readFile:  deps.ReadFile,
	}
}

// Layer identifies the knowledge layer served by the adapter.
func (a *filesAdapter) Layer() Layer { return LayerFiles }

// filesRecallTimeout bounds one files-layer HybridSearch (query embed + scan),
// matching filesemindex.semindexQueryTimeout so a slow embed cannot stall the
// agent knowledge recall behind the embedding client's 30s HTTP timeout.
const filesRecallTimeout = 8 * time.Second

// Recall runs the index's hybrid (BM25 lexical + dense cosine) search and maps
// each hit to a knowledge.Result. The hybrid search already applies the
// embedder-calibrated cosine floor (filestore.minSemanticScore) as its
// admission gate, so an off-topic
// query returns zero hits here — over-recall protection lives in the index, not
// in the caller. A down/unhealthy embedder yields an empty slice (never an
// error), matching every other recall source's degradation.
func (a *filesAdapter) Recall(ctx context.Context, query string, limit int) ([]Result, error) {
	if a.index == nil || a.embed == nil || !a.embed.IsHealthy() {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	qctx, cancel := context.WithTimeout(ctx, filesRecallTimeout)
	defer cancel()
	hits, err := a.index.HybridSearch(qctx, query, limit, a.embed, a.extractFn)
	if err != nil {
		// Timeout / cancel → sentinel so Router can surface a degrade note
		// instead of looking like "no file knowledge".
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || qctx.Err() != nil {
			return nil, ErrFilesRecallTimeout
		}
		return nil, err
	}
	out := make([]Result, 0, len(hits))
	for _, h := range hits {
		meta := map[string]string{}
		if h.StartLine > 0 {
			meta["startLine"] = fmt.Sprintf("%d", h.StartLine)
			meta["endLine"] = fmt.Sprintf("%d", h.EndLine)
		}
		out = append(out, Result{
			Ref:     Ref{Layer: LayerFiles, ID: h.Entry.PathDisplay},
			Snippet: strings.TrimSpace(h.Snippet),
			// h.Score is the best-chunk cosine (a stable 0–1 number in the
			// BGE-M3 band), surfaced as the file's similarity. The Router merges
			// across layers by this score; see chat recall preflight for the
			// per-layer quota that keeps files from crowding out wiki/diary.
			Score: h.Score,
			Meta:  meta,
			Time:  fileMTimeMillis(h.Entry.ServerModified),
		})
	}
	return out, nil
}

// Read fetches the full extracted text behind a file ref. The hybrid recall
// hands back a single matching chunk; an agent that wants the whole document
// calls knowledge(op="read", ref="f:<path>"). Returns an error when no file
// reader is wired (recall is unaffected) or the file is gone.
func (a *filesAdapter) Read(ctx context.Context, id string) (*Document, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("empty file ref")
	}
	if a.readFile == nil {
		return nil, fmt.Errorf("file layer cannot read full documents (no reader wired)")
	}
	data, name, err := a.readFile(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", id, err)
	}
	content := ""
	if a.extractFn != nil {
		content = strings.TrimSpace(a.extractFn(ctx, data, name))
	}
	if content == "" {
		content = "(no extractable text)"
	}
	title := name
	if title == "" {
		title = pathBaseName(id)
	}
	return &Document{
		Ref:     Ref{Layer: LayerFiles, ID: id},
		Title:   title,
		Content: content,
		Meta:    map[string]string{"path": id},
	}, nil
}

// fileMTimeMillis converts an RFC3339 ServerModified string to unix-milli, or 0
// when it is empty/unparseable (Result.Time's documented "no timestamp" value).
func fileMTimeMillis(serverModified string) int64 {
	serverModified = strings.TrimSpace(serverModified)
	if serverModified == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, serverModified)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// pathBaseName returns the last "/"-segment of a virtual file path.
func pathBaseName(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
