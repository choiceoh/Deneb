package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/liteparse"
)

// processFetchedContent classifies fetched payloads and routes each type
// to the correct extraction/formatting path.
func processFetchedContent(
	ctx context.Context,
	rawContent string,
	rawBytes []byte,
	contentType string,
	url string,
	sglang *SGLangExtractor,
	meta *webFetchMeta,
) string {
	switch classifyContentType(contentType) {
	case contentTypeHTML:
		return processHTML(ctx, rawContent, url, sglang, meta)
	case contentTypeJSON:
		return processJSON(rawContent)
	case contentTypeDocument:
		// Use raw bytes (not charset-normalized string) for binary documents.
		return processDocument(ctx, rawBytes, url)
	default:
		return rawContent
	}
}

type fetchedContentType string

const (
	contentTypePlain    fetchedContentType = "plain"
	contentTypeHTML     fetchedContentType = "html"
	contentTypeJSON     fetchedContentType = "json"
	contentTypeDocument fetchedContentType = "document"
)

func classifyContentType(contentType string) fetchedContentType {
	switch {
	case strings.Contains(contentType, "text/html"),
		strings.Contains(contentType, "application/xhtml"):
		return contentTypeHTML
	case strings.Contains(contentType, "application/json"),
		strings.Contains(contentType, "+json"):
		return contentTypeJSON
	case liteparse.Available() && liteparse.SupportedMIME(contentType):
		return contentTypeDocument
	default:
		return contentTypePlain
	}
}

// processJSON pretty-prints JSON for readability.
func processJSON(raw string) string {
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return raw // invalid JSON — return as-is
	}
	pretty, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return raw
	}
	return string(pretty)
}

// processDocument extracts text from a binary document (PDF, Office, etc.)
// using the LiteParse CLI. Falls back to a notice if parsing fails.
func processDocument(ctx context.Context, data []byte, url string) string {
	name := "document"
	if url != "" {
		// Extract filename from URL path.
		if idx := strings.LastIndex(url, "/"); idx >= 0 {
			name = url[idx+1:]
		}
		// Strip query string.
		if idx := strings.Index(name, "?"); idx >= 0 {
			name = name[:idx]
		}
	}

	text, err := liteparse.Parse(ctx, data, name)
	if err != nil {
		return fmt.Sprintf("(문서 파싱 실패: %s)", err)
	}
	if strings.TrimSpace(text) == "" {
		return "(문서에서 텍스트를 추출하지 못했습니다)"
	}
	return text
}
