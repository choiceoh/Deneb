// Package ffi provides RPC method handlers for all Rust FFI-backed methods.
//
// This consolidates protocol validation, security, media, parsing, memory
// search, markdown, compaction, context engine, Vega, and ML handlers that
// delegate to the deneb-core Rust library via CGo FFI.
package ffi

import (
	"context"
	"encoding/json"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// VegaDeps holds the Vega backend dependency for Vega FFI RPC methods.
type VegaDeps struct {
	Backend vega.Backend
}

// ---------------------------------------------------------------------------
// Protocol
// ---------------------------------------------------------------------------

// ProtocolMethods returns handlers for protocol validation RPC methods.
func ProtocolMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"protocol.validate":        protocolValidate(),
		"protocol.validate_params": protocolValidateParams(),
	}
}

func protocolValidate() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Frame string `json:"frame"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Frame == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "frame is required"))
		}
		err := ffipkg.ValidateFrame(p.Frame)
		backend := "go-fallback"
		if ffipkg.Available {
			backend = "rust"
		}
		if err != nil {
			return protocol.MustResponseOK(req.ID, map[string]any{
				"valid": false, "error": err.Error(), "backend": backend,
			})
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"valid": true, "backend": backend,
		})
	}
}

func protocolValidateParams() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Method string `json:"method"`
			Params string `json:"params"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.Method == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "method is required"))
		}
		if p.Params == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "params is required"))
		}
		valid, errorsJSON, err := ffipkg.ValidateParams(p.Method, p.Params)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		backend := "go-fallback"
		if ffipkg.Available {
			backend = "rust"
		}
		result := map[string]any{"valid": valid, "backend": backend}
		if errorsJSON != nil {
			result["errors"] = json.RawMessage(errorsJSON)
		}
		return protocol.MustResponseOK(req.ID, result)
	}
}

// ---------------------------------------------------------------------------
// Security
// ---------------------------------------------------------------------------

// SecurityMethods returns handlers for security-related RPC methods.
func SecurityMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"security.validate_session_key": securityValidateSessionKey(),
		"security.sanitize_html":        securitySanitizeHTML(),
		"security.is_safe_url":          securityIsSafeURL(),
		"security.validate_error_code":  securityValidateErrorCode(),
	}
}

func securityValidateSessionKey() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key string `json:"key"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		err := ffipkg.ValidateSessionKey(p.Key)
		return protocol.MustResponseOK(req.ID, map[string]any{
			"valid": err == nil,
		})
	}
}

func securitySanitizeHTML() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Input string `json:"input"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"output": ffipkg.SanitizeHTML(p.Input),
		})
	}
}

func securityIsSafeURL() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			URL string `json:"url"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"safe": ffipkg.IsSafeURL(p.URL),
		})
	}
}

func securityValidateErrorCode() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Code string `json:"code"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"valid": ffipkg.ValidateErrorCode(p.Code),
		})
	}
}

// ---------------------------------------------------------------------------
// Media
// ---------------------------------------------------------------------------

// MediaMethods returns handlers for media detection RPC methods.
func MediaMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"media.detect_mime": mediaDetectMIME(),
	}
}

func mediaDetectMIME() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Data []byte `json:"data"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"mime": ffipkg.DetectMIME(p.Data),
		})
	}
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

// ParsingMethods returns handlers for parsing RPC methods (pre-LLM heavy parsing).
func ParsingMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"parsing.extract_links":       parsingExtractLinks(),
		"parsing.html_to_markdown":    parsingHtmlToMarkdown(),
		"parsing.base64_estimate":     parsingBase64Estimate(),
		"parsing.base64_canonicalize": parsingBase64Canonicalize(),
		"parsing.media_tokens":        parsingMediaTokens(),
	}
}

func parsingExtractLinks() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Text     string `json:"text"`
			MaxLinks int    `json:"max_links"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.MaxLinks <= 0 {
			p.MaxLinks = 5
		}
		urls, err := ffipkg.ExtractLinks(p.Text, p.MaxLinks)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"urls": urls,
		})
	}
}

func parsingHtmlToMarkdown() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			HTML string `json:"html"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		text, title, err := ffipkg.HtmlToMarkdown(p.HTML)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		result := map[string]any{"text": text}
		if title != "" {
			result["title"] = title
		}
		return protocol.MustResponseOK(req.ID, result)
	}
}

func parsingBase64Estimate() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Input string `json:"input"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		estimated, err := ffipkg.Base64Estimate(p.Input)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"estimated_bytes": estimated,
		})
	}
}

func parsingBase64Canonicalize() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Input string `json:"input"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		canonical, err := ffipkg.Base64Canonicalize(p.Input)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"canonical": canonical,
		})
	}
}

func parsingMediaTokens() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Text string `json:"text"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		cleanText, mediaURLs, audioAsVoice, err := ffipkg.ParseMediaTokens(p.Text)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		result := map[string]any{"text": cleanText}
		if len(mediaURLs) > 0 {
			result["media_urls"] = mediaURLs
			result["media_url"] = mediaURLs[0]
		}
		if audioAsVoice {
			result["audio_as_voice"] = true
		}
		return protocol.MustResponseOK(req.ID, result)
	}
}

// ---------------------------------------------------------------------------
// Memory search (Rust SIMD-accelerated)
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Markdown (Rust pulldown-cmark parser)
// ---------------------------------------------------------------------------

// MarkdownMethods returns handlers for markdown processing RPC methods.
func MarkdownMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"markdown.to_ir":          markdownToIR(),
		"markdown.detect_fences":  markdownDetectFences(),
	}
}

func markdownToIR() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Markdown string `json:"markdown"`
			Options  string `json:"options"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		ir, err := ffipkg.MarkdownToIR(p.Markdown, p.Options)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		// ir is already JSON; wrap in the response directly.
		return protocol.MustResponseOK(req.ID, json.RawMessage(ir))
	}
}

func markdownDetectFences() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Text string `json:"text"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		fences, err := ffipkg.MarkdownDetectFences(p.Text)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"fences": fences,
		})
	}
}

// ---------------------------------------------------------------------------
// Compaction (Rust context compression engine)
// ---------------------------------------------------------------------------

// CompactionMethods returns handlers for compaction RPC methods.
func CompactionMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"compaction.evaluate":   compactionEvaluate(),
		"compaction.sweep.new":  compactionSweepNew(),
		"compaction.sweep.start": compactionSweepStart(),
		"compaction.sweep.step": compactionSweepStep(),
		"compaction.sweep.drop": compactionSweepDrop(),
	}
}

func compactionEvaluate() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Config       string `json:"config"`
			StoredTokens uint64 `json:"stored_tokens"`
			LiveTokens   uint64 `json:"live_tokens"`
			TokenBudget  uint64 `json:"token_budget"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.Config == "" {
			p.Config = `{"contextThreshold":0.75}`
		}
		result, err := ffipkg.CompactionEvaluate(p.Config, p.StoredTokens, p.LiveTokens, p.TokenBudget)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func compactionSweepNew() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Config         string `json:"config"`
			ConversationID uint64 `json:"conversation_id"`
			TokenBudget    uint64 `json:"token_budget"`
			Force          bool   `json:"force"`
			HardTrigger    bool   `json:"hard_trigger"`
			NowMs          int64  `json:"now_ms"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.Config == "" {
			p.Config = `{"contextThreshold":0.75}`
		}
		handle, err := ffipkg.CompactionSweepNew(p.Config, p.ConversationID, p.TokenBudget, p.Force, p.HardTrigger, p.NowMs)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{"handle": handle})
	}
}

func compactionSweepStart() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		result, err := ffipkg.CompactionSweepStart(p.Handle)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func compactionSweepStep() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle   uint32          `json:"handle"`
			Response json.RawMessage `json:"response"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		if len(p.Response) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "response is required"))
		}
		result, err := ffipkg.CompactionSweepStep(p.Handle, p.Response)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func compactionSweepDrop() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		ffipkg.CompactionSweepDrop(p.Handle)
		return protocol.MustResponseOK(req.ID, map[string]any{"dropped": true})
	}
}

// ---------------------------------------------------------------------------
// Context Engine (Rust FFI state-machine based)
// ---------------------------------------------------------------------------

// ContextEngineMethods returns handlers for context engine RPC methods.
func ContextEngineMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"context.assembly.new":   contextAssemblyNew(),
		"context.assembly.start": contextAssemblyStart(),
		"context.assembly.step":  contextAssemblyStep(),
		"context.expand.new":     contextExpandNew(),
		"context.expand.start":   contextExpandStart(),
		"context.expand.step":    contextExpandStep(),
		"context.engine.drop":    contextEngineDrop(),
	}
}

func contextAssemblyNew() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ConversationID uint64 `json:"conversation_id"`
			TokenBudget    uint64 `json:"token_budget"`
			FreshTailCount uint32 `json:"fresh_tail_count"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		handle, err := ffipkg.ContextAssemblyNew(p.ConversationID, p.TokenBudget, p.FreshTailCount)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{"handle": handle})
	}
}

func contextAssemblyStart() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		result, err := ffipkg.ContextAssemblyStart(p.Handle)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func contextAssemblyStep() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle   uint32          `json:"handle"`
			Response json.RawMessage `json:"response"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		if len(p.Response) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "response is required"))
		}
		result, err := ffipkg.ContextAssemblyStep(p.Handle, p.Response)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func contextExpandNew() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			SummaryID       string `json:"summary_id"`
			MaxDepth        uint32 `json:"max_depth"`
			IncludeMessages bool   `json:"include_messages"`
			TokenCap        uint64 `json:"token_cap"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.SummaryID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "summary_id is required"))
		}
		handle, err := ffipkg.ContextExpandNew(p.SummaryID, p.MaxDepth, p.IncludeMessages, p.TokenCap)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{"handle": handle})
	}
}

func contextExpandStart() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		result, err := ffipkg.ContextExpandStart(p.Handle)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func contextExpandStep() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle   uint32          `json:"handle"`
			Response json.RawMessage `json:"response"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		if len(p.Response) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "response is required"))
		}
		result, err := ffipkg.ContextExpandStep(p.Handle, p.Response)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func contextEngineDrop() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		ffipkg.ContextEngineDrop(p.Handle)
		return protocol.MustResponseOK(req.ID, map[string]any{"dropped": true})
	}
}

// ---------------------------------------------------------------------------
// Vega FFI (Rust FFI — requires "vega" feature in deneb-core)
// ---------------------------------------------------------------------------

// VegaMethods returns handlers for Vega FFI RPC methods.
// The deps parameter provides the Vega backend for command execution.
func VegaMethods(deps VegaDeps) map[string]rpcutil.HandlerFunc {
	m := map[string]rpcutil.HandlerFunc{
		"vega.ffi.execute": vegaFFIExecute(),
		"vega.ffi.search":  vegaFFISearch(),
	}

	// Register backend-forwarding commands if a backend is available.
	if deps.Backend != nil {
		vegaCommands := map[string]string{
			"vega.ask":         "ask",
			"vega.update":      "update",
			"vega.add-action":  "add-action",
			"vega.mail-append": "mail-append",
			"vega.version":     "version",
		}
		for method, cmd := range vegaCommands {
			m[method] = vegaBackendHandler(deps.Backend, cmd)
		}
	}

	return m
}

func vegaFFIExecute() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "params required"))
		}
		result, err := ffipkg.VegaExecute(string(req.Params))
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func vegaFFISearch() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "params required"))
		}
		result, err := ffipkg.VegaSearch(string(req.Params))
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

// vegaBackendHandler creates an RPC handler that executes a Vega command
// via the Backend interface (Rust FFI).
func vegaBackendHandler(backend vega.Backend, cmd string) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		// Parse params as a generic map for the Backend.Execute call.
		var args map[string]any
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &args); err != nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrMissingParam, "invalid params: "+err.Error()))
			}
		}

		result, err := backend.Execute(ctx, cmd, args)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "vega: "+err.Error()))
		}

		return protocol.MustResponseOKRaw(req.ID, result)
	}
}

// ---------------------------------------------------------------------------
