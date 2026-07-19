// filestore_semindex.go — semantic (vector) search over the on-box file store.
//
// One shared *filestore.SemanticIndex is built here and reused by three call
// sites so they all see the same vectors:
//   - the background reindex task (semindexTask) — keeps the index fresh,
//   - the chat files tool (CoreToolDeps.FilesSemanticSearch),
//   - the miniapp.files.search RPC (FilesBrowseDeps.SemanticSearch).
//
// The index embeds each file's extracted text once (embedding sidecar) and ranks files by
// the best chunk cosine similarity to the query — finding files by meaning, not
// just literal substring. Everything degrades silently when the embedding
// sidecar is down: reindex is a no-op and search returns empty, so the callers
// fall back to name/content search.
package filesemindex

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/document"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

const (
	// semindexInterval is how often the index is refreshed against the store.
	// Incremental (only new/changed files re-embed), so a cycle is cheap once the
	// index is warm; 15 min keeps newly added files findable without churn. The
	// autonomous service also runs the first cycle ~initialGrace after boot.
	semindexInterval = 15 * time.Minute
	// semindexRunTimeout bounds one reindex pass. Generous headroom kept from
	// the CPU BGE-M3 era (the GPU Nemotron backend is much faster); this runs
	// off the request path, so the slack costs nothing.
	semindexRunTimeout = 10 * time.Minute
	// semindexQueryTimeout bounds a single search (one query embed + cosine scan),
	// keeping the RPC/tool call responsive even if the embedding server is slow.
	semindexQueryTimeout = 8 * time.Second
)

// fileSemindexPath returns the sidecar index location under the state dir
// (DENEB_STATE_DIR-aware, so a dev gateway never writes into production ~/.deneb).
// It lives outside the store root so it never shows up in a file listing.
func fileSemindexPath() string {
	return filepath.Join(config.ResolveStateDir(), "files-semindex.json")
}

// Service owns the shared file store, semantic index, and embedding client.
type Service struct {
	index     *filestore.SemanticIndex
	store     filestore.Store
	embedding *embedding.Client
	logger    *slog.Logger
}

// New opens the semantic sidecar for store. A nil store disables the service.
func New(store filestore.Store, embeddingClient *embedding.Client, logger *slog.Logger) *Service {
	if store == nil {
		return nil
	}
	path := fileSemindexPath()
	if logger != nil {
		logger.Info("file semantic index enabled", "path", path)
	}
	return &Service{
		index:     filestore.NewSemanticIndex(path),
		store:     store,
		embedding: embeddingClient,
		logger:    logger,
	}
}

// fileSemanticSearch ranks store files by meaning. Returns an empty slice (never
// an error to the caller's fallback logic) when the index/embedding server is
// unavailable. Bounded by semindexQueryTimeout so a slow embed never stalls a
// chat turn or RPC.
//
// It uses the index's HYBRID search: BM25 lexical overlap (file name + extracted
// text) fused with dense-vector cosine, admitting a file when either signal is
// convincing. This keeps the calibrated semantic floor's clean rejection of
// Korean noise while letting exact name/content matches survive below the floor —
// strictly better than the cosine-only Search for the user-facing "의미" mode, so
// that one mode now covers lexical+semantic in a single call (no extra flag).
//
// Results are validated against the live store with Stat, dropping any hit whose
// path no longer exists. The index is reindexed only every 15 minutes, and the
// move/delete hooks (Rename/Remove) cover the in-process mutations — but this
// Stat backstop also catches paths that vanished by any other route (a direct
// filesystem delete, an external mount change), so a search never hands back a
// path that would 404 at download time.
func (s *Service) Search(ctx context.Context, query string, max int) ([]filestore.ScoredEntry, error) {
	if s.index == nil || s.embedding == nil {
		return nil, nil
	}
	qctx, cancel := context.WithTimeout(ctx, semindexQueryTimeout)
	defer cancel()
	hits, err := s.index.HybridSearch(qctx, query, max, s.embedding, fileSemindexExtract)
	if err != nil || len(hits) == 0 || s.store == nil {
		return hits, err
	}
	live := hits[:0] // reuse backing array; we only ever shrink
	for _, h := range hits {
		if _, serr := s.store.Stat(qctx, h.Entry.PathDisplay); serr == nil {
			live = append(live, h)
		}
	}
	return live, nil
}

// fileIndexRemove drops a path's vectors from the semantic index immediately
// (between 15-min reindex passes). Wired into the files RPC delete path AND
// the overwrite-save path (an in-place replace leaves stale-content vectors;
// dropping them beats ranking the file by text it no longer contains — the
// next reindex re-embeds it). No-op when the index isn't enabled.
func (s *Service) Remove(path string) {
	if s.index == nil {
		return
	}
	s.index.Remove(path)
}

// fileIndexRename re-keys a moved path in the semantic index immediately. Wired
// into the files RPC move path. No-op when the index isn't enabled.
func (s *Service) Rename(oldPath, newPath string) {
	if s.index == nil {
		return
	}
	s.index.Rename(oldPath, newPath)
}

// fileSemindexExtract is the text extractor the reindex passes to the index —
// the chat tools' shared document extractor (PDF/Office/text/image OCR). Wired
// here (the server layer may import tools); the domain takes it as a callback to
// avoid a layer inversion.
func fileSemindexExtract(ctx context.Context, data []byte, name string) string {
	t, _ := document.ExtractDocumentText(ctx, data, name, "")
	return t
}

// registerFileSemindexTask registers the background reindex PeriodicTask. No-op
// when the index/store/embedding client isn't wired. Called during
// registerWorkflowSideEffects (the non-RPC phase, alongside modeltuner etc.).
func (s *Service) Task() autonomous.PeriodicTask {
	if s == nil || s.index == nil || s.store == nil || s.embedding == nil {
		return nil
	}
	return &semindexTask{
		index:     s.index,
		store:     s.store,
		embedding: s.embedding,
		logger:    s.logger,
	}
}

// semindexTask implements autonomous.PeriodicTask: it keeps the file semantic
// index in sync with the store. Incremental + degradation-safe (a down embedding
// server makes Run a quiet no-op).
type semindexTask struct {
	index     *filestore.SemanticIndex
	store     filestore.Store
	embedding *embedding.Client
	logger    *slog.Logger
}

// Name returns the component's stable scheduler name.
func (t *semindexTask) Name() string { return "file-semindex" }

// Interval returns the component's scheduling cadence.
func (t *semindexTask) Interval() time.Duration { return semindexInterval }

// Run does one incremental reindex pass. It owns its own generous deadline
// (off the request path). A down embedding server is a quiet no-op, not an error
// — the index simply stays as-is until the server returns.
func (t *semindexTask) Run(ctx context.Context) error {
	if t.embedding == nil || !t.embedding.IsHealthy() {
		return nil // embedding server down → skip silently, retry next cycle
	}
	rctx, cancel := context.WithTimeout(ctx, semindexRunTimeout)
	defer cancel()

	stats, err := t.index.Reindex(rctx, t.store, fileSemindexExtract, t.embedding)
	if err != nil {
		// A partial pass still persisted what it embedded; the next cycle resumes.
		// Warn (not Error): no user-facing failure — search degrades to name match.
		if t.logger != nil {
			t.logger.Warn("file semindex reindex incomplete",
				"error", err, "scanned", stats.Scanned, "embedded", stats.Embedded)
		}
		return err
	}
	if t.logger != nil && (stats.Embedded > 0 || stats.Removed > 0) {
		t.logger.Info("file semindex updated",
			"scanned", stats.Scanned, "embedded", stats.Embedded,
			"removed", stats.Removed, "skipped", stats.Skipped)
	}
	return nil
}

// Compile-time interface compliance.
var _ autonomous.PeriodicTask = (*semindexTask)(nil)

// newFilesKnowledgeAdapter wraps the shared file semantic index as a
// knowledge.Adapter so the unified knowledge(recall/read) tool federates over
// uploaded files alongside the wiki. Returns nil when the index or embedding
// client is unavailable (knowledge.NewFilesAdapter enforces this too), so the
// Router drops the file layer and recall degrades to wiki-only. The injected
// ReadFile closure backs knowledge(op="read", ref="f:<path>") by fetching the
// file bytes; extraction reuses the same document extractor as the index.
func (s *Service) KnowledgeAdapter() knowledge.Adapter {
	if s.index == nil || s.embedding == nil {
		return nil
	}
	var readFile func(ctx context.Context, path string) ([]byte, string, error)
	if s.store != nil {
		store := s.store
		readFile = func(ctx context.Context, path string) ([]byte, string, error) {
			data, ent, err := store.Get(ctx, path)
			if err != nil {
				return nil, "", err
			}
			name := path
			if ent != nil && ent.Name != "" {
				name = ent.Name
			}
			return data, name, nil
		}
	}
	return knowledge.NewFilesAdapter(knowledge.FilesAdapterDeps{
		Index:     s.index,
		Embed:     s.embedding,
		ExtractFn: fileSemindexExtract,
		ReadFile:  readFile,
	})
}

// fileRecallForPreflight adapts the file semantic search into the chat recall
// preflight's transport-neutral FileRecallFunc. It reuses fileSemanticSearch
// (hybrid search + live-store Stat validation + the query timeout), then maps
// each hit to chatport.FileRecallHit. Returns nil-safe empty on any degradation
// (no index/embedding server, an embed error) so the preflight's files source
// simply contributes nothing — never an error, never a stall. Wired into
// HandlerConfig.FileRecallFn only when the index is enabled.
func (s *Service) Recall(ctx context.Context, query string, limit int) []chatport.FileRecallHit {
	if s.index == nil || s.embedding == nil {
		return nil
	}
	hits, err := s.Search(ctx, query, limit)
	if err != nil || len(hits) == 0 {
		return nil
	}
	out := make([]chatport.FileRecallHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, chatport.FileRecallHit{
			Path:       h.Entry.PathDisplay,
			Snippet:    h.Snippet,
			StartLine:  h.StartLine,
			EndLine:    h.EndLine,
			Score:      h.Score,
			ModifiedAt: fileServerModifiedMillis(h.Entry.ServerModified),
		})
	}
	return out
}

// fileServerModifiedMillis converts an Entry.ServerModified RFC3339 string to
// unix-milli, or 0 when empty/unparseable.
func fileServerModifiedMillis(serverModified string) int64 {
	if serverModified == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, serverModified)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}
