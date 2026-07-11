package document

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	attachmentPerDocRunes = 20000
	attachmentTotalRunes  = 48000
	attachmentConcurrency = 4
)

// Attachment is the format-neutral payload consumed by ExtractAttachments.
type Attachment struct {
	Filename string
	MimeType string
	Bytes    []byte
}

// ExtractAttachments extracts attachments concurrently while preserving input
// order and enforcing bounded per-document and aggregate output sizes.
func ExtractAttachments(ctx context.Context, attachments []Attachment) string {
	texts := make([]string, len(attachments))
	var wg sync.WaitGroup
	sem := make(chan struct{}, attachmentConcurrency)
	for i := range attachments {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return renderAttachments(attachments, texts)
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() { _ = recover() }()
			a := attachments[i]
			texts[i] = ExtractAttachmentText(ctx, a.Bytes, a.Filename, a.MimeType)
		}(i)
	}
	wg.Wait()
	return renderAttachments(attachments, texts)
}

func renderAttachments(attachments []Attachment, texts []string) string {
	var b strings.Builder
	total := 0
	for i, attachment := range attachments {
		fmt.Fprintf(&b, "### 📎 %s (%s)\n", attachment.Filename, attachment.MimeType)
		text := texts[i]
		if text == "" {
			b.WriteString("(텍스트를 추출하지 못했습니다 — 스캔 품질이 낮거나 지원하지 않는 형식일 수 있습니다.)\n\n")
			continue
		}
		remaining := attachmentTotalRunes - total
		if remaining <= 0 {
			b.WriteString("(이하 첨부는 전체 분량 한도로 생략 — 개별 첨부로 다시 요청하세요.)\n")
			break
		}
		limit := min(attachmentPerDocRunes, remaining)
		clipped := clipRunes(text, limit)
		b.WriteString(clipped)
		b.WriteString("\n\n")
		total += utf8.RuneCountInString(clipped)
	}
	return strings.TrimSpace(b.String())
}

func clipRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + " …(생략 — 전체가 필요하면 더 좁은 첨부 선택자로 다시 요청하세요.)"
}
