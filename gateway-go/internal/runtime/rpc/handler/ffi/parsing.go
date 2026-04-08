package ffi

import (
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
	type params struct {
		Text     string `json:"text"`
		MaxLinks int    `json:"max_links"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.MaxLinks <= 0 {
			p.MaxLinks = 5
		}
		urls, err := ffipkg.ExtractLinks(p.Text, p.MaxLinks)
		if err != nil {
			return nil, rpcerr.Wrap(protocol.ErrInvalidRequest, err)
		}
		return map[string]any{"urls": urls}, nil
	})
}

func parsingHTMLToMarkdown() rpcutil.HandlerFunc {
	type params struct {
		HTML string `json:"html"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		text, title, err := ffipkg.HTMLToMarkdown(p.HTML)
		if err != nil {
			return nil, rpcerr.Wrap(protocol.ErrInvalidRequest, err)
		}
		result := map[string]any{"text": text}
		if title != "" {
			result["title"] = title
		}
		return result, nil
	})
}

func parsingBase64Estimate() rpcutil.HandlerFunc {
	type params struct {
		Input string `json:"input"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		estimated, err := ffipkg.Base64Estimate(p.Input)
		if err != nil {
			return nil, rpcerr.Wrap(protocol.ErrInvalidRequest, err)
		}
		return map[string]any{"estimated_bytes": estimated}, nil
	})
}

func parsingBase64Canonicalize() rpcutil.HandlerFunc {
	type params struct {
		Input string `json:"input"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		canonical, err := ffipkg.Base64Canonicalize(p.Input)
		if err != nil {
			return nil, rpcerr.Wrap(protocol.ErrInvalidRequest, err)
		}
		return map[string]any{"canonical": canonical}, nil
	})
}

func parsingMediaTokens() rpcutil.HandlerFunc {
	type params struct {
		Text string `json:"text"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		cleanText, mediaURLs, audioAsVoice, err := ffipkg.ParseMediaTokens(p.Text)
		if err != nil {
			return nil, rpcerr.Wrap(protocol.ErrInvalidRequest, err)
		}
		result := map[string]any{"text": cleanText}
		if len(mediaURLs) > 0 {
			result["media_urls"] = mediaURLs
			result["media_url"] = mediaURLs[0]
		}
		if audioAsVoice {
			result["audio_as_voice"] = true
		}
		return result, nil
	})
}
