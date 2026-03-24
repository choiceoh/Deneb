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
	d.Register("system.info", systemInfo())
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
}

func healthCheck(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"status":       "ok",
			"runtime":      "go",
			"ffi":          ffi.Available,
			"sessions":     deps.Sessions.Count(),
			"channels":     deps.Channels.List(),
		})
		return resp
	}
}

func sessionsList(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp, _ := protocol.NewResponseOK(req.ID, deps.Sessions.List())
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
		resp, _ := protocol.NewResponseOK(req.ID, s)
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
		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"deleted": found})
		return resp
	}
}

func channelsList(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp, _ := protocol.NewResponseOK(req.ID, deps.Channels.List())
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
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
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
		resp, _ := protocol.NewResponseOK(req.ID, deps.Channels.StatusAll())
		return resp
	}
}

func systemInfo() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"runtime":      "go",
			"version":      "0.1.0",
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
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"valid": false, "error": err.Error(), "backend": backend,
			})
			return resp
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
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
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"mime": ffi.DetectMIME(p.Data),
		})
		return resp
	}
}

func channelsHealth(deps Deps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.ChannelLifecycle == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"channels": []any{},
			})
			return resp
		}
		health := deps.ChannelLifecycle.HealthCheck()
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
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
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
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
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
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
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
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
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
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
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
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
		resp, _ := protocol.NewResponseOK(req.ID, result)
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
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
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
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
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
		resp, _ := protocol.NewResponseOK(req.ID, result)
		return resp
	}
}
