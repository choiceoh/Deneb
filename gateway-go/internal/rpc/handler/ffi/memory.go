package ffi

import (
	"context"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
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
		p, errResp := rpcutil.DecodeParams[struct {
			A []float64 `json:"a"`
			B []float64 `json:"b"`
		}](req)
		if errResp != nil {
			return errResp
		}
		similarity := ffipkg.MemoryCosineSimilarity(p.A, p.B)
		return rpcutil.RespondOK(req.ID, map[string]any{
			"similarity": similarity,
		})
	}
}

func memoryBm25RankToScore() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Rank float64 `json:"rank"`
		}](req)
		if errResp != nil {
			return errResp
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"score": ffipkg.MemoryBm25RankToScore(p.Rank),
		})
	}
}

func memoryBuildFtsQuery() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Raw string `json:"raw"`
		}](req)
		if errResp != nil {
			return errResp
		}
		query, err := ffipkg.MemoryBuildFtsQuery(p.Raw)
		if err != nil {
			return rpcerr.Wrap(protocol.ErrInvalidRequest, err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"query": query,
		})
	}
}

func memoryMergeHybridResults() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return rpcerr.New(protocol.ErrMissingParam, "params required").Response(req.ID)
		}
		results, err := ffipkg.MemoryMergeHybridResults(string(req.Params))
		if err != nil {
			return rpcerr.Wrap(protocol.ErrInvalidRequest, err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"results": results,
		})
	}
}

func memoryExtractKeywords() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Query string `json:"query"`
		}](req)
		if errResp != nil {
			return errResp
		}
		keywords, err := ffipkg.MemoryExtractKeywords(p.Query)
		if err != nil {
			return rpcerr.Wrap(protocol.ErrInvalidRequest, err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"keywords": keywords,
		})
	}
}
