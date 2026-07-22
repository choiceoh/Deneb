// run_attachments_image.go — attached-image understanding for the capture path.
// An image is understood by the vision-capable model chain (main model → the
// dedicated vision model) and only falls back to OCR glyph extraction when no
// vision model is available or the vision call fails — so a chart, diagram,
// photo, or handwriting is described, not flattened to whatever text it happens
// to contain (the old OCR-only path returned almost nothing for such images).
package chat

import (
	"context"
	"encoding/base64"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
)

const imageDescribeTokens = 900

const imageDescribePrompt = "너는 첨부 이미지 분석기다. 이미지를 한국어로 설명한다. " +
	"무엇이 담겼는지(사진·도표·차트·표·스크린샷·손글씨 등)와 핵심 내용을 사실 위주로 압축한다. " +
	"차트·표·수식은 항목·수치·관계를 최대한 정확히 읽어낸다. 이미지 안의 글자는 원문 그대로 옮긴다. " +
	"해석·추측·서론 없이 내용만 출력한다."

// visionDescriber matches pilot.DescribeImage; injected so the OCR-fallback logic
// is testable without a live vision model.
type visionDescriber func(ctx context.Context, system, userText, mimeType, imageBase64 string, maxTokens int) string

// DescribeCapturedImage returns a text understanding of an attached image: the
// vision model chain (main → dedicated vision model) describes it, and only when
// no vision model is available or the describe fails/empties does it fall back to
// OCR glyph extraction (ocr, PaddleOCR). Returns "" only when every tier fails —
// the caller then skips the image. ocr may be nil (no OCR fallback wired).
func DescribeCapturedImage(ctx context.Context, img []byte, mime string, ocr func(context.Context, []byte) (string, error)) string {
	return describeCapturedImage(ctx, img, mime, ocr, pilot.DescribeImage)
}

func describeCapturedImage(ctx context.Context, img []byte, mime string, ocr func(context.Context, []byte) (string, error), describe visionDescriber) string {
	if len(img) == 0 {
		return ""
	}
	if desc := describe(ctx, imageDescribePrompt, "", mime, base64.StdEncoding.EncodeToString(img), imageDescribeTokens); strings.TrimSpace(desc) != "" {
		return strings.TrimSpace(desc)
	}
	if ocr != nil {
		text, err := ocr(ctx, img)
		if err != nil {
			slog.Warn("image describe: OCR fallback failed", "error", err)
			return ""
		}
		return strings.TrimSpace(text)
	}
	return ""
}
