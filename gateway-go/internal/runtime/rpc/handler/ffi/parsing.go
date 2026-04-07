package ffi

import (
	"context"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ai/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ParsingMethods returns handlers for parsing RPC methods (pre-LLM heavy parsing).
func ParsingMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"parsing.extract_links":       parsingExtractLinks(),
		"parsing.html_to_markdown":    parsingHTMLToMarkdown(),
		"parsing.base64_estimate":     parsingBase64Estimate(),
		"parsing.base64_canonicalize": parsingBase64Canonicalize(),
		"parsing.media_tokens":        parsingMediaTokens(),
	}
}

func parsingExtractLinks() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Text     string `json:"text"`
			MaxLinks int    `json:"max_links"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.MaxLinks <= 0 {
			p.MaxLinks = 5
		}
		urls, err := ffipkg.ExtractLinks(p.Text, p.MaxLinks)
		if err != nil {
			return rpcerr.Wrap(protocol.ErrInvalidRequest, err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"urls": urls,
		})
	}
}

func parsingHTMLToMarkdown() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			HTML string `json:"html"`
		}](req)
		if errResp != nil {
			return errResp
		}
		text, title, err := ffipkg.HTMLToMarkdown(p.HTML)
		if err != nil {
			return rpcerr.Wrap(protocol.ErrInvalidRequest, err).Response(req.ID)
		}
		result := map[string]any{"text": text}
		if title != "" {
			result["title"] = title
		}
		return rpcutil.RespondOK(req.ID, result)
	}
}

func parsingBase64Estimate() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Input string `json:"input"`
		}](req)
		if errResp != nil {
			return errResp
		}
		estimated, err := ffipkg.Base64Estimate(p.Input)
		if err != nil {
			return rpcerr.Wrap(protocol.ErrInvalidRequest, err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"estimated_bytes": estimated,
		})
	}
}

func parsingBase64Canonicalize() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Input string `json:"input"`
		}](req)
		if errResp != nil {
			return errResp
		}
		canonical, err := ffipkg.Base64Canonicalize(p.Input)
		if err != nil {
			return rpcerr.Wrap(protocol.ErrInvalidRequest, err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"canonical": canonical,
		})
	}
}

func parsingMediaTokens() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Text string `json:"text"`
		}](req)
		if errResp != nil {
			return errResp
		}
		cleanText, mediaURLs, audioAsVoice, err := ffipkg.ParseMediaTokens(p.Text)
		if err != nil {
			return rpcerr.Wrap(protocol.ErrInvalidRequest, err).Response(req.ID)
		}
		result := map[string]any{"text": cleanText}
		if len(mediaURLs) > 0 {
			result["media_urls"] = mediaURLs
			result["media_url"] = mediaURLs[0]
		}
		if audioAsVoice {
			result["audio_as_voice"] = true
		}
		return rpcutil.RespondOK(req.ID, result)
	}
}
