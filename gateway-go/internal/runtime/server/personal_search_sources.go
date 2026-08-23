package server

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	miniknowledge "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind"
)

const (
	personalSearchFileSnippetRunes = 480
	personalSearchMailSnippetRunes = 600
	personalSearchLexicalTimeout   = 10 * time.Second
)

type personalMailSearcher interface {
	Search(context.Context, string, int) ([]gmail.MessageSummary, error)
}

type personalMailMirror interface {
	Len() int
	SearchContext(context.Context, []string, string, time.Time, int) []mailarchive.ContextMessage
}

// searchPersonalFiles merges exact name/content matches with the shared hybrid
// index. Running both branches matters: a healthy semantic index can still omit
// a newly mounted, oversized, externally modified, or not-yet-indexed file.
func searchPersonalFiles(
	ctx context.Context,
	store filestore.Store,
	semantic func(context.Context, string, int) ([]filestore.ScoredEntry, error),
	query string,
	limit int,
) ([]miniknowledge.SearchFileHit, error) {
	if store == nil {
		return nil, miniknowledge.ErrSearchSourceUnavailable
	}

	type branchResult struct {
		hits    []miniknowledge.SearchFileHit
		err     error
		lexical bool
	}
	branches := 1
	results := make(chan branchResult, 2)
	var wg sync.WaitGroup
	if semantic != nil {
		branches++
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := semantic(ctx, query, limit)
			results <- branchResult{hits: projectSemanticFileHits(rows, limit), err: err}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		lexicalCtx, cancel := context.WithTimeout(ctx, personalSearchLexicalTimeout)
		defer cancel()
		hits, err := searchPersonalFilesLexically(lexicalCtx, store, query, limit)
		results <- branchResult{hits: hits, err: err, lexical: true}
	}()

	var (
		lexicalHits  []miniknowledge.SearchFileHit
		semanticHits []miniknowledge.SearchFileHit
		lexicalErr   error
		semanticErr  error
		completed    int
	)
	for range branches {
		select {
		case result := <-results:
			if result.lexical {
				lexicalErr = result.err
				lexicalHits = append(lexicalHits, result.hits...)
			} else {
				semanticErr = result.err
				semanticHits = append(semanticHits, result.hits...)
			}
			if result.err == nil {
				completed++
			}
		case <-ctx.Done():
			merged := mergePersonalFileHits(lexicalHits, semanticHits, limit)
			if completed > 0 || len(merged) > 0 {
				return merged, miniknowledge.ErrSearchSourcePartial
			}
			return nil, ctx.Err()
		}
	}
	wg.Wait()
	merged := mergePersonalFileHits(lexicalHits, semanticHits, limit)
	failed := 0
	if lexicalErr != nil {
		failed++
	}
	if semantic != nil && semanticErr != nil {
		failed++
	}
	if failed > 0 && (completed > 0 || len(merged) > 0) {
		// A semantic hit does not prove that exact, newly mounted files were
		// searched, and a lexical hit does not replace semantic recall. Preserve
		// useful evidence while telling every client the source is incomplete.
		return merged, miniknowledge.ErrSearchSourcePartial
	}
	if errors.Is(lexicalErr, miniknowledge.ErrSearchSourcePartial) || errors.Is(lexicalErr, filestore.ErrContentSearchPartial) {
		return merged, miniknowledge.ErrSearchSourcePartial
	}
	if failed == 0 {
		return merged, nil
	}
	if lexicalErr != nil {
		return nil, lexicalErr
	}
	return nil, semanticErr
}

func searchPersonalFilesLexically(ctx context.Context, store filestore.Store, query string, limit int) ([]miniknowledge.SearchFileHit, error) {
	coverageIncomplete := false
	evidenceIncomplete := false
	normalizedQuery := strings.ToLower(strings.TrimSpace(query))
	extract := func(ctx context.Context, data []byte, name string) string {
		text, complete := toolbind.ExtractFileTextWithoutOCR(ctx, name, data)
		if !complete {
			coverageIncomplete = true
		}
		return text
	}
	type completeContentSearcher interface {
		SearchContentComplete(context.Context, string, int, func(context.Context, []byte, string) string) ([]filestore.Entry, error)
	}
	var entries []filestore.Entry
	var err error
	if complete, ok := store.(completeContentSearcher); ok {
		entries, err = complete.SearchContentComplete(ctx, query, limit, extract)
	} else {
		entries, err = store.SearchContent(ctx, query, limit, extract)
	}
	// SearchContent returns rows accumulated before cancellation. Preserve those
	// exact hits together with the error so the caller reports a partial source
	// instead of silently presenting an incomplete scan as successful.
	hits := make([]miniknowledge.SearchFileHit, 0, len(entries))
	for _, entry := range entries {
		text := ""
		nameMatched := strings.Contains(strings.ToLower(entry.Name), normalizedQuery)
		// SearchContent deliberately treats an oversized filename hit as valid
		// without reading its bytes. Keep that safety boundary when building
		// evidence so a 100GB filename match cannot become an os.ReadFile OOM.
		if ctx.Err() == nil && entry.Size <= filestore.MaxContentExtractBytes {
			if data, getErr := readPersonalSearchEvidence(ctx, store, entry.PathDisplay); getErr == nil {
				text = extract(ctx, data, entry.Name)
			} else if ctxErr := ctx.Err(); ctxErr != nil {
				return hits, ctxErr
			} else {
				evidenceIncomplete = true
			}
		}
		// A content-only row may disappear or change between the scan and the
		// evidence read. Never relabel that race as a stronger filename hit: drop
		// the unverified row and expose partial coverage instead.
		if !nameMatched && !strings.Contains(strings.ToLower(text), normalizedQuery) {
			evidenceIncomplete = true
			continue
		}
		snippet, startLine, endLine := personalSearchLexicalEvidence(text, query)
		score := 1.0
		if nameMatched {
			score = 1.05 // exact filename match remains stronger than semantic similarity
		}
		hits = append(hits, miniknowledge.SearchFileHit{
			Path:      entry.PathDisplay,
			Name:      entry.Name,
			Size:      entry.Size,
			Snippet:   snippet,
			Score:     score,
			StartLine: startLine,
			EndLine:   endLine,
			Kind:      personalSearchFileKind(entry.Name),
		})
	}
	if err != nil {
		return hits, err
	}
	if coverageIncomplete || evidenceIncomplete {
		return hits, miniknowledge.ErrSearchSourcePartial
	}
	return hits, nil
}

func readPersonalSearchEvidence(ctx context.Context, store filestore.Store, path string) ([]byte, error) {
	r, meta, err := store.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if meta != nil && meta.Size > filestore.MaxContentExtractBytes {
		return nil, filestore.ErrContentSearchPartial
	}
	data, err := io.ReadAll(io.LimitReader(r, filestore.MaxContentExtractBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > filestore.MaxContentExtractBytes {
		return nil, filestore.ErrContentSearchPartial
	}
	return data, nil
}

func mergePersonalFileHits(exact, semantic []miniknowledge.SearchFileHit, limit int) []miniknowledge.SearchFileHit {
	if limit <= 0 {
		limit = 20
	}
	out := make([]miniknowledge.SearchFileHit, 0, min(limit, len(exact)+len(semantic)))
	seen := make(map[string]struct{}, len(exact)+len(semantic))
	appendUnique := func(hits []miniknowledge.SearchFileHit) {
		for _, hit := range hits {
			key := strings.ToLower(strings.TrimSpace(hit.Path))
			if key == "" {
				key = strings.ToLower(strings.TrimSpace(hit.Name))
			}
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, hit)
			if len(out) >= limit {
				return
			}
		}
	}
	appendUnique(exact)
	if len(out) < limit {
		appendUnique(semantic)
	}
	return out
}

func projectSemanticFileHits(rows []filestore.ScoredEntry, limit int) []miniknowledge.SearchFileHit {
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	hits := make([]miniknowledge.SearchFileHit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, miniknowledge.SearchFileHit{
			Path:      row.Entry.PathDisplay,
			Name:      row.Entry.Name,
			Size:      row.Entry.Size,
			Snippet:   truncatePersonalSearchText(strings.TrimSpace(row.Snippet), personalSearchFileSnippetRunes),
			Score:     row.Score,
			StartLine: row.StartLine,
			EndLine:   row.EndLine,
			Kind:      row.Kind,
			Heading:   row.Heading,
		})
	}
	return hits
}

func searchPersonalMail(ctx context.Context, client personalMailSearcher, query string, limit int) ([]miniknowledge.SearchMailHit, error) {
	if client == nil {
		return nil, miniknowledge.ErrSearchSourceUnavailable
	}
	rows, err := client.Search(ctx, query, limit)
	if err != nil && !errors.Is(err, mailarchive.ErrArchivePartial) {
		return nil, err
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	hits := make([]miniknowledge.SearchMailHit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, miniknowledge.SearchMailHit{
			ID:       row.ID,
			ThreadID: row.ThreadID,
			From:     row.From,
			Subject:  row.Subject,
			Date:     row.Date,
			Snippet:  truncatePersonalSearchText(strings.TrimSpace(row.Snippet), personalSearchMailSnippetRunes),
			Mailbox:  row.Mailbox,
		})
	}
	if errors.Is(err, mailarchive.ErrArchivePartial) {
		return hits, miniknowledge.ErrSearchSourcePartial
	}
	return hits, nil
}

func searchPersonalMailMirror(ctx context.Context, store personalMailMirror, query string, limit int) ([]miniknowledge.SearchMailHit, error) {
	if store == nil || store.Len() == 0 {
		return nil, miniknowledge.ErrSearchSourceUnavailable
	}
	rows := store.SearchContext(ctx, nil, query, time.Time{}, limit)
	hits := make([]miniknowledge.SearchMailHit, 0, len(rows))
	for _, row := range rows {
		snippet, _, _ := personalSearchLexicalEvidence(row.Body, query)
		if snippet == "" {
			snippet = row.Snippet
		}
		id := strings.TrimSpace(row.ID)
		if id == "" {
			id = strings.TrimSpace(row.Locator)
		}
		hits = append(hits, miniknowledge.SearchMailHit{
			ID:      id,
			From:    row.From,
			Subject: row.Subject,
			Date:    row.Date,
			Snippet: truncatePersonalSearchText(strings.TrimSpace(snippet), personalSearchMailSnippetRunes),
			Mailbox: row.Mailbox,
		})
	}
	if ctx.Err() != nil {
		return hits, miniknowledge.ErrSearchSourcePartial
	}
	return hits, nil
}

func personalSearchLexicalEvidence(text, query string) (string, int, int) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || strings.TrimSpace(text) == "" {
		return "", 0, 0
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if !strings.Contains(strings.ToLower(line), query) {
			continue
		}
		from, to := i, i+1
		if from > 0 {
			from--
		}
		if to < len(lines) {
			to++
		}
		snippet := truncatePersonalSearchText(strings.TrimSpace(strings.Join(lines[from:to], "\n")), personalSearchFileSnippetRunes)
		return snippet, i + 1, i + 1
	}
	return "", 0, 0
}

func personalSearchFileKind(name string) string {
	kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if kind == "" {
		return "file"
	}
	return kind
}

func truncatePersonalSearchText(text string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	marker := []rune("…")
	runes := []rune(text)
	return string(runes[:maxRunes-len(marker)]) + string(marker)
}
