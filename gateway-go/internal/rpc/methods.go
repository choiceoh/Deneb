package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds the subsystems that built-in RPC methods need.
type Deps struct {
	Sessions         *session.Manager
	Channels         *channel.Registry
	ChannelLifecycle *channel.LifecycleManager
	GatewaySubs      *events.GatewayEventSubscriptions
	Version          string // Server version string (from --version flag).
}

// unmarshalParams safely unmarshals request params, handling nil/empty params.
func unmarshalParams(params json.RawMessage, v any) error {
	if len(params) == 0 {
		return errors.New("missing params")
	}
	return json.Unmarshal(params, v)
}

// maxKeyInErrorMsg is the maximum key length included in error messages.
// Prevents log inflation from pathologically large keys.
const maxKeyInErrorMsg = 128

// truncateForError truncates a string for safe inclusion in error messages.
func truncateForError(s string) string {
	if len(s) <= maxKeyInErrorMsg {
		return s
	}
	return s[:maxKeyInErrorMsg] + "..."
}

// RegisterBuiltinMethods registers the core Go-native RPC methods on the
// dispatcher. Methods handled here don't need to be forwarded to Node.js.
func RegisterBuiltinMethods(d *Dispatcher, deps Deps) {
	d.Register("health.check", healthCheck(deps))
	d.Register("sessions.list", sessionsList(deps))
	d.Register("sessions.get", sessionsGet(deps))
	d.Register("sessions.delete", sessionsDelete(deps))
	d.Register("channels.list", channelsList(deps))
	d.Register("channels.get", channelsGet(deps))
	d.Register("channels.status", channelsStatus(deps))
	d.Register("system.info", systemInfo(deps))
	d.Register("channels.health", channelsHealth(deps))
	d.Register("protocol.validate", protocolValidate())
	// Note: constant_time_eq is intentionally not exposed as an RPC method
	// to prevent use as a secret comparison oracle.
	d.Register("security.validate_session_key", securityValidateSessionKey())
	d.Register("security.sanitize_html", securitySanitizeHTML())
	d.Register("security.is_safe_url", securityIsSafeURL())
	d.Register("security.validate_error_code", securityValidateErrorCode())
	d.Register("media.detect_mime", mediaDetectMIME())
	d.Register("parsing.extract_links", parsingExtractLinks())
	d.Register("parsing.html_to_markdown", parsingHtmlToMarkdown())
	d.Register("parsing.base64_estimate", parsingBase64Estimate())
	d.Register("parsing.base64_canonicalize", parsingBase64Canonicalize())
	d.Register("parsing.media_tokens", parsingMediaTokens())

	// Memory search methods (Rust SIMD-accelerated algorithms).
	d.Register("memory.cosine_similarity", memoryCosineSimilarity())
	d.Register("memory.bm25_rank_to_score", memoryBm25RankToScore())
	d.Register("memory.build_fts_query", memoryBuildFtsQuery())
	d.Register("memory.merge_hybrid_results", memoryMergeHybridResults())
	d.Register("memory.extract_keywords", memoryExtractKeywords())

	// Markdown IR processing methods (Rust pulldown-cmark parser).
	d.Register("markdown.to_ir", markdownToIR())
	d.Register("markdown.detect_fences", markdownDetectFences())

	// Protocol parameter validation (Rust schema validators).
	d.Register("protocol.validate_params", protocolValidateParams())

	// Compaction methods (Rust context compression engine).
	d.Register("compaction.evaluate", compactionEvaluate())
	d.Register("compaction.sweep.new", compactionSweepNew())
	d.Register("compaction.sweep.start", compactionSweepStart())
	d.Register("compaction.sweep.step", compactionSweepStep())
	d.Register("compaction.sweep.drop", compactionSweepDrop())

	// Context engine methods (Rust FFI state-machine based).
	d.Register("context.assembly.new", contextAssemblyNew())
	d.Register("context.assembly.start", contextAssemblyStart())
	d.Register("context.assembly.step", contextAssemblyStep())
	d.Register("context.expand.new", contextExpandNew())
	d.Register("context.expand.start", contextExpandStart())
	d.Register("context.expand.step", contextExpandStep())
	d.Register("context.engine.drop", contextEngineDrop())

	// Vega FFI methods (requires "vega" feature in Rust core).
	d.Register("vega.ffi.execute", vegaFFIExecute())
	d.Register("vega.ffi.search", vegaFFISearch())

	// ML methods (requires "ml" feature in Rust core).
	d.Register("ml.embed", mlEmbed())
	d.Register("ml.rerank", mlRerank())

	// Tools catalog (static core tool definitions).
	d.Register("tools.catalog", toolsCatalog())
}

func healthCheck(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"status":   "ok",
			"runtime":  "go",
			"ffi":      ffi.Available,
			"sessions": deps.Sessions.Count(),
			"channels": deps.Channels.List(),
		})
		return resp
	}
}

func sessionsList(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, deps.Sessions.List())
		return resp
	}
}

func sessionsGet(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key string `json:"key"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.Key == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "key is required"))
		}
		s := deps.Sessions.Get(p.Key)
		if s == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "session not found: "+truncateForError(p.Key)))
		}
		resp := protocol.MustResponseOK(req.ID, s)
		return resp
	}
}

func sessionsDelete(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key   string `json:"key"`
			Force bool   `json:"force"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.Key == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "key is required"))
		}
		// Check if session is running (prevent accidental deletion).
		s := deps.Sessions.Get(p.Key)
		if s != nil && s.Status == session.StatusRunning && !p.Force {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrConflict, "session is currently running; use force=true to delete"))
		}
		found := deps.Sessions.Delete(p.Key)
		if found && deps.GatewaySubs != nil {
			deps.GatewaySubs.EmitLifecycle(events.LifecycleChangeEvent{
				SessionKey: p.Key,
				Reason:     "deleted",
			})
		}
		resp := protocol.MustResponseOK(req.ID, map[string]bool{"deleted": found})
		return resp
	}
}

func channelsList(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, deps.Channels.List())
		return resp
	}
}

func channelsGet(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}
		ch := deps.Channels.Get(p.ID)
		if ch == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "channel not found: "+truncateForError(p.ID)))
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"id":           ch.ID(),
			"meta":         ch.Meta(),
			"capabilities": ch.Capabilities(),
			"status":       ch.Status(),
		})
		return resp
	}
}

func channelsStatus(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, deps.Channels.StatusAll())
		return resp
	}
}

func systemInfo(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		version := deps.Version
		if version == "" {
			version = "unknown"
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"runtime":      "go",
			"version":      version,
			"goVersion":    runtime.Version(),
			"os":           runtime.GOOS,
			"arch":         runtime.GOARCH,
			"numCPU":       runtime.NumCPU(),
			"ffiAvailable": ffi.Available,
		})
		return resp
	}
}

// protocolValidate exposes Rust frame validation via RPC.
func protocolValidate() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Frame string `json:"frame"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.Frame == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "frame is required"))
		}
		err := ffi.ValidateFrame(p.Frame)
		backend := "go-fallback"
		if ffi.Available {
			backend = "rust"
		}
		if err != nil {
			resp := protocol.MustResponseOK(req.ID, map[string]any{
				"valid": false, "error": err.Error(), "backend": backend,
			})
			return resp
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"valid": true, "backend": backend,
		})
		return resp
	}
}

func mediaDetectMIME() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Data []byte `json:"data"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"mime": ffi.DetectMIME(p.Data),
		})
		return resp
	}
}

func channelsHealth(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.ChannelLifecycle == nil {
			resp := protocol.MustResponseOK(req.ID, map[string]any{
				"channels": []any{},
			})
			return resp
		}
		health := deps.ChannelLifecycle.HealthCheck()
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"channels": health,
		})
		return resp
	}
}

func securityValidateSessionKey() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key string `json:"key"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		err := ffi.ValidateSessionKey(p.Key)
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"valid": err == nil,
		})
		return resp
	}
}

func securitySanitizeHTML() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Input string `json:"input"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"output": ffi.SanitizeHTML(p.Input),
		})
		return resp
	}
}

func securityIsSafeURL() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			URL string `json:"url"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"safe": ffi.IsSafeURL(p.URL),
		})
		return resp
	}
}

func securityValidateErrorCode() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Code string `json:"code"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"valid": ffi.ValidateErrorCode(p.Code),
		})
		return resp
	}
}

// ---------------------------------------------------------------------------
// Parsing RPC methods (pre-LLM heavy parsing)
// ---------------------------------------------------------------------------

func parsingExtractLinks() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Text     string `json:"text"`
			MaxLinks int    `json:"max_links"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.MaxLinks <= 0 {
			p.MaxLinks = 5
		}
		urls, err := ffi.ExtractLinks(p.Text, p.MaxLinks)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"urls": urls,
		})
		return resp
	}
}

func parsingHtmlToMarkdown() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			HTML string `json:"html"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		text, title, err := ffi.HtmlToMarkdown(p.HTML)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		result := map[string]any{"text": text}
		if title != "" {
			result["title"] = title
		}
		resp := protocol.MustResponseOK(req.ID, result)
		return resp
	}
}

func parsingBase64Estimate() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Input string `json:"input"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		estimated, err := ffi.Base64Estimate(p.Input)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"estimated_bytes": estimated,
		})
		return resp
	}
}

func parsingBase64Canonicalize() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Input string `json:"input"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		canonical, err := ffi.Base64Canonicalize(p.Input)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"canonical": canonical,
		})
		return resp
	}
}

func parsingMediaTokens() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Text string `json:"text"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		cleanText, mediaURLs, audioAsVoice, err := ffi.ParseMediaTokens(p.Text)
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
		resp := protocol.MustResponseOK(req.ID, result)
		return resp
	}
}

// ---------------------------------------------------------------------------
// Memory search RPC methods (Rust SIMD-accelerated)
// ---------------------------------------------------------------------------

func memoryCosineSimilarity() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			A []float64 `json:"a"`
			B []float64 `json:"b"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		similarity := ffi.MemoryCosineSimilarity(p.A, p.B)
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"similarity": similarity,
		})
		return resp
	}
}

func memoryBm25RankToScore() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Rank float64 `json:"rank"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"score": ffi.MemoryBm25RankToScore(p.Rank),
		})
		return resp
	}
}

func memoryBuildFtsQuery() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Raw string `json:"raw"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		query, err := ffi.MemoryBuildFtsQuery(p.Raw)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"query": query,
		})
		return resp
	}
}

func memoryMergeHybridResults() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "params required"))
		}
		results, err := ffi.MemoryMergeHybridResults(string(req.Params))
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"results": results,
		})
		return resp
	}
}

func memoryExtractKeywords() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Query string `json:"query"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		keywords, err := ffi.MemoryExtractKeywords(p.Query)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"keywords": keywords,
		})
		return resp
	}
}

// ---------------------------------------------------------------------------
// Markdown RPC methods (Rust pulldown-cmark parser)
// ---------------------------------------------------------------------------

func markdownToIR() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Markdown string `json:"markdown"`
			Options  string `json:"options"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		ir, err := ffi.MarkdownToIR(p.Markdown, p.Options)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		// ir is already JSON; wrap in the response directly.
		resp := protocol.MustResponseOK(req.ID, json.RawMessage(ir))
		return resp
	}
}

func markdownDetectFences() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Text string `json:"text"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		fences, err := ffi.MarkdownDetectFences(p.Text)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"fences": fences,
		})
		return resp
	}
}

// ---------------------------------------------------------------------------
// Protocol parameter validation RPC method
// ---------------------------------------------------------------------------

func protocolValidateParams() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Method string `json:"method"`
			Params string `json:"params"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
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
		valid, errorsJSON, err := ffi.ValidateParams(p.Method, p.Params)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		backend := "go-fallback"
		if ffi.Available {
			backend = "rust"
		}
		result := map[string]any{"valid": valid, "backend": backend}
		if errorsJSON != nil {
			result["errors"] = json.RawMessage(errorsJSON)
		}
		resp := protocol.MustResponseOK(req.ID, result)
		return resp
	}
}
