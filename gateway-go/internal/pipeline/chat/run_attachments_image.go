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

// visionAttachmentMinBytes is the floor for spending a vision call on an ATTACHED
// image (the mail / chat attachment path, not the capture path — a user who
// attaches something in chat meant it, so it always gets vision).
//
// Mail arrives unattended and in bulk: every company logo, banner and stamped
// signature glyph in a footer would otherwise cost a frontier vision call per
// message. Those are small; a photographed or scanned business document is not
// (a phone photo of a 견적서 runs hundreds of KB to a few MB). The mail gate
// already drops anything under 2KB as a tracker pixel — this floor is the same
// idea one tier up, and images below it still get read, just by local OCR.
const visionAttachmentMinBytes = 40 << 10

// AttachmentExtractor is the format-neutral extraction signature shared by the
// mail-analysis and chat attachment paths (mailanalysis.PipelineDeps.AttachmentExtractFn).
type AttachmentExtractor func(ctx context.Context, data []byte, filename, mimeType string) string

// WithImageVision upgrades an attachment extractor so that an attached IMAGE is
// understood by the vision chain first, instead of being flattened to OCR glyphs.
//
// Why: the capture path (miniapp.capture.*) has described images with the vision
// model since #4100, but everything that arrives through an *extractor* — mail
// attachments, andromeda/OpenAI-compatible chat attachments — went straight to
// PaddleOCR. So the same photographed 견적서 read one way in chat and another way
// in mail, and a photo whose meaning is not glyphs (a site photo, a stamped
// drawing, handwriting) came back nearly empty from the mail path.
//
// Non-images, small images, and any image the vision tiers cannot read fall
// through to the wrapped extractor, which already OCRs images — so this can only
// add understanding, never remove it. No OCR fallback is passed to the describer
// for exactly that reason: the wrapped extractor is the fallback, and running OCR
// in both places would read the same image twice.
func WithImageVision(extract AttachmentExtractor) AttachmentExtractor {
	return withImageVision(extract, pilot.DescribeImage)
}

// withImageVision is WithImageVision with the vision tier injected, so the
// routing can be tested without a live model (same seam as describeCapturedImage).
func withImageVision(extract AttachmentExtractor, describe visionDescriber) AttachmentExtractor {
	if extract == nil {
		return nil
	}
	return func(ctx context.Context, data []byte, filename, mimeType string) string {
		if worthVisionAttachment(data, filename, mimeType) {
			if desc := describeCapturedImage(ctx, data, mimeType, nil, describe); desc != "" {
				// Logged because this path is otherwise invisible: the mail analysis
				// just gets better text. One line per image attachment (rare) makes
				// "did vision actually read it, or did it fall through to OCR?"
				// answerable from the journal.
				slog.Info("attachment image described by vision", "file", filename, "bytes", len(data), "chars", len([]rune(desc)))
				return desc
			}
		}
		return extract(ctx, data, filename, mimeType)
	}
}

// worthVisionAttachment reports whether an attachment is an image big enough to
// be worth a vision call. MIME is authoritative when present; a filename
// extension covers the senders that ship images as octet-stream.
func worthVisionAttachment(data []byte, filename, mimeType string) bool {
	if len(data) < visionAttachmentMinBytes {
		return false
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "image/") {
		return true
	}
	lower := strings.ToLower(filename)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".tif", ".tiff", ".heic", ".heif"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
