package ffi

import (
	"context"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

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
