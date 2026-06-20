// filestore_semindex.go — semantic (vector) search over the on-box file store.
//
// One shared *filestore.SemanticIndex is built here and reused by three call
// sites so they all see the same vectors:
//   - the background reindex task (semindexTask) — keeps the index fresh,
//   - the chat files tool (CoreToolDeps.FilesSemanticSearch),
//   - the miniapp.files.search RPC (FilesBrowseDeps.SemanticSearch).
//
// The index embeds each file's extracted text once (BGE-M3) and ranks files by
// the best chunk cosine similarity to the query — finding files by meaning, not
// just literal substring. Everything degrades silently when the embedding server
// (:8001) is down: reindex is a no-op and search returns empty, so the callers
// fall back to name/content search.
package server

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

const (
	// semindexInterval is how often the index is refreshed against the store.
	// Incremental (only new/changed files re-embed), so a cycle is cheap once the
	// index is warm; 15 min keeps newly added files findable without churn. The
	// autonomous service also runs the first cycle ~initialGrace after boot.
	semindexInterval = 15 * time.Minute
	// semindexRunTimeout bounds one reindex pass. Generous because the CPU BGE-M3
	// server is slow under host load and this runs off the request path.
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

// initFileSemanticIndex opens the shared file store and its semantic index, and
// wires the search closure into the chat files tool deps. Called from
// initToolsAndDeps after s.embeddingClient exists. Safe when the store can't be
// opened (features just stay off) or the embedding client is nil (search/reindex
// degrade to empty). The background reindex task is registered separately in
// registerWorkflowSideEffects via registerFileSemindexTask.
func (s *Server) initFileSemanticIndex() {
	if s.toolDeps == nil {
		return
	}
	s.fileStore = localFileStoreOrNil(s.logger)
	if s.fileStore == nil {
		return // no store → no semantic search (RPC/tool fall back to name search)
	}
	s.fileSemanticIndex = filestore.NewSemanticIndex(fileSemindexPath())

	// Share one search closure; nil embedding client (or an unhealthy server)
	// makes it return an empty slice, so callers fall back to name/content search.
	s.toolDeps.FilesSemanticSearch = s.fileSemanticSearch
	s.logger.Info("file semantic index enabled", "path", fileSemindexPath())
}

// fileSemanticSearch ranks store files by meaning. Returns an empty slice (never
// an error to the caller's fallback logic) when the index/embedding server is
// unavailable. Bounded by semindexQueryTimeout so a slow embed never stalls a
// chat turn or RPC.
//
// Results are validated against the live store with Stat, dropping any hit whose
// path no longer exists. The index is reindexed only every 15 minutes, and the
// move/delete hooks (Rename/Remove) cover the in-process mutations — but this
// Stat backstop also catches paths that vanished by any other route (a direct
// filesystem delete, an external mount change), so a search never hands back a
// path that would 404 at download time.
func (s *Server) fileSemanticSearch(ctx context.Context, query string, max int) ([]filestore.ScoredEntry, error) {
	if s.fileSemanticIndex == nil || s.embeddingClient == nil {
		return nil, nil
	}
	qctx, cancel := context.WithTimeout(ctx, semindexQueryTimeout)
	defer cancel()
	hits, err := s.fileSemanticIndex.Search(qctx, query, max, s.embeddingClient)
	if err != nil || len(hits) == 0 || s.fileStore == nil {
		return hits, err
	}
	live := hits[:0] // reuse backing array; we only ever shrink
	for _, h := range hits {
		if _, serr := s.fileStore.Stat(qctx, h.Entry.PathDisplay); serr == nil {
			live = append(live, h)
		}
	}
	return live, nil
}

// fileIndexRemove drops a deleted/moved-away path from the semantic index
// immediately (between 15-min reindex passes). Wired into the files RPC delete
// path. No-op when the index isn't enabled.
func (s *Server) fileIndexRemove(path string) {
	if s.fileSemanticIndex == nil {
		return
	}
	s.fileSemanticIndex.Remove(path)
}

// fileIndexRename re-keys a moved path in the semantic index immediately. Wired
// into the files RPC move path. No-op when the index isn't enabled.
func (s *Server) fileIndexRename(oldPath, newPath string) {
	if s.fileSemanticIndex == nil {
		return
	}
	s.fileSemanticIndex.Rename(oldPath, newPath)
}

// fileSemindexExtract is the text extractor the reindex passes to the index —
// the chat tools' shared document extractor (PDF/Office/text/image OCR). Wired
// here (the server layer may import tools); the domain takes it as a callback to
// avoid a layer inversion.
func fileSemindexExtract(ctx context.Context, data []byte, name string) string {
	t, _ := tools.ExtractDocumentText(ctx, data, name, "")
	return t
}

// registerFileSemindexTask registers the background reindex PeriodicTask. No-op
// when the index/store/embedding client isn't wired. Called during
// registerWorkflowSideEffects (the non-RPC phase, alongside modeltuner etc.).
func (s *Server) registerFileSemindexTask() {
	if s.fileSemanticIndex == nil || s.fileStore == nil || s.embeddingClient == nil || s.autonomousSvc == nil {
		return
	}
	s.autonomousSvc.RegisterTask(&semindexTask{
		index:     s.fileSemanticIndex,
		store:     s.fileStore,
		embedding: s.embeddingClient,
		logger:    s.logger,
	})
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

func (t *semindexTask) Name() string            { return "file-semindex" }
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
