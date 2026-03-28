package ffi

import (
	"context"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MemoryMethods returns handlers for memory search RPC methods.
func MemoryMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"memory.cosine_similarity":    memoryCosineSimilarity(),
		"memory.bm25_rank_to_score":   memoryBm25RankToScore(),
		"memory.build_fts_query":      memoryBuildFtsQuery(),
		"memory.merge_hybrid_results": memoryMergeHybridResults(),
		"memory.extract_keywords":     memoryExtractKeywords(),
	}
}

func memoryCosineSimilarity() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			A []float64 `json:"a"`
			B []float64 `json:"b"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		similarity := ffipkg.MemoryCosineSimilarity(p.A, p.B)
		return protocol.MustResponseOK(req.ID, map[string]any{
			"similarity": similarity,
		})
	}
}

func memoryBm25RankToScore() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Rank float64 `json:"rank"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"score": ffipkg.MemoryBm25RankToScore(p.Rank),
		})
	}
}

func memoryBuildFtsQuery() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Raw string `json:"raw"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		query, err := ffipkg.MemoryBuildFtsQuery(p.Raw)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"query": query,
		})
	}
}

func memoryMergeHybridResults() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "params required"))
		}
		results, err := ffipkg.MemoryMergeHybridResults(string(req.Params))
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"results": results,
		})
	}
}

func memoryExtractKeywords() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Query string `json:"query"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		keywords, err := ffipkg.MemoryExtractKeywords(p.Query)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"keywords": keywords,
		})
	}
}
