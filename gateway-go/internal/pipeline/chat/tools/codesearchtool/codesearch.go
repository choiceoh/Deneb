package codesearchtool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	airerank "github.com/choiceoh/deneb/gateway-go/internal/ai/rerank"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/codesearch"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolCodeSearch wraps semantic code search over symbols plus tracked repository
// file chunks. Dense, CodeGraph FTS, and local BM25 candidates are RRF-fused and
// reranked; the result is expanded into a bounded context pack containing actual
// source, safe adjacent relations, and applicable repository docs.
//
// The embedder (:8002) and reranker (:8004) self-wire from env exactly as the
// codesearch CLI does — no dep threading. The index lives in <repo>/.codegraph
// (built by `make codesearch-index`); when absent, the tool returns guidance
// instead of an error (expected state on a fresh checkout).
func ToolCodeSearch(workspaceDir string) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Query string `json:"query"`
			K     int    `json:"k"`
		}
		if err := jsonutil.UnmarshalInto("code_search params", input, &p); err != nil {
			return "", err
		}
		if strings.TrimSpace(p.Query) == "" {
			return "", fmt.Errorf("query is required")
		}
		k := p.K
		if k <= 0 {
			k = 10
		}
		k = min(k, 20)

		dir := resolveCodeIndexDir(workspaceDir)
		if dir == "" {
			return "시맨틱 코드 인덱스(.codegraph/semantic-code.json)가 아직 없습니다 — `make codesearch-index`로 빌드하거나, 구조·관계 질문이면 `codegraph_explore`를 쓰세요.", nil
		}

		url := os.Getenv("DENEB_EMBEDDING_URL")
		if url == "" {
			url = "http://127.0.0.1:8002"
		}
		emb := embedding.New(url, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
		repo := filepath.Dir(dir)
		hits, err := codesearch.SearchRanked(ctx, repo, dir, emb, codeSearchReranker(), p.Query, k)
		if err != nil {
			return "", err
		}
		if len(hits) == 0 {
			return fmt.Sprintf("\"%s\"에 대한 시맨틱 매치 없음. 키워드가 명확하면 grep, 구조/호출 관계면 codegraph_explore를 시도하세요.", p.Query), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "code_search \"%s\" — 상위 %d개 (dense+BM25+FTS 융합, 리랭크)\n\n", p.Query, len(hits))
		b.WriteString(codesearch.BuildContextPack(ctx, repo, dir, p.Query, hits))
		return b.String(), nil
	}
}

// resolveCodeIndexDir returns the .codegraph dir holding a built semantic index,
// or "" if none. Preference: DENEB_CODESEARCH_DIR, then the workspace, then an
// ancestor walk from the workspace (the repo root often holds the index).
func resolveCodeIndexDir(workspaceDir string) string {
	if env := os.Getenv("DENEB_CODESEARCH_DIR"); env != "" {
		if hasSemanticIndex(env) {
			return env
		}
	}
	for d := workspaceDir; d != "" && d != "/"; d = filepath.Dir(d) {
		cand := filepath.Join(d, ".codegraph")
		if hasSemanticIndex(cand) {
			return cand
		}
	}
	return ""
}

func hasSemanticIndex(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "semantic-code.json"))
	return err == nil
}

func codeSearchReranker() codesearch.Reranker {
	if c := airerank.NewFromEnv(); c != nil {
		return c
	}
	return nil
}
