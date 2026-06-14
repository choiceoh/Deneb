// deals.go — upserts a deal/transaction wiki page from a structured
// business-document extraction (견적서/계약서/세금계산서/거래명세서 등).
//
// One page per counterparty under 프로젝트/거래/<slug>.md — a raw-data sub-folder
// of the 프로젝트 category (거래 folded into 프로젝트 in the 5-category taxonomy),
// accumulating each document as a dated entry in a "## 거래 문서" log. Idempotent:
// a document already filed
// (matched by its SourceRef) is a no-op, so re-analyzing the same mail never
// double-appends or bumps the Updated date. Mirrors the contacts.go enrichment
// precedent (createPersonPage / enrichPersonPage / upsertSection).
package wiki

import (
	"fmt"
	"strings"
	"time"
)

const dealDocsHeading = "거래 문서"

// dealCategoryDir is the raw-data sub-folder under 프로젝트 where per-counterparty
// deal pages live (거래 is a 프로젝트 sub-folder, not a top-level category).
const dealCategoryDir = "프로젝트/거래"

// DealPageInput is the structured business-document data filed onto a deal
// page. Counterparty is required (it keys the page); every other field is
// optional and omitted from the rendered entry when empty.
type DealPageInput struct {
	Counterparty string   // 거래처/회사명 — page key (required)
	DocType      string   // 견적서·계약서·세금계산서·거래명세서 등
	Amount       string   // 금액 (free-text: "5,000,000원", "$1,200")
	Date         string   // 문서 일자 (free-text or YYYY-MM-DD)
	DueDate      string   // 납기·결제 기한 (YYYY-MM-DD when known) → page Due
	Items        []string // 주요 품목/항목
	Summary      string   // 한 줄 요약
	SourceRef    string   // provenance/dedup key (e.g. "mail:<id>")
}

// UpsertDealPage files a business document onto its counterparty's deal page,
// creating the page when absent. Returns the page path and whether a new page
// was created. Idempotent by SourceRef: a document already recorded is a no-op
// (created=false, no write). now is injected for deterministic tests.
func (s *Store) UpsertDealPage(in DealPageInput, now time.Time) (relPath string, created bool, err error) {
	counterparty := strings.TrimSpace(in.Counterparty)
	if counterparty == "" {
		return "", false, fmt.Errorf("wiki: deal page requires a counterparty")
	}
	slug := dealSlug(counterparty)
	if slug == "" {
		return "", false, fmt.Errorf("wiki: counterparty %q slugs to empty", counterparty)
	}
	relPath = dealCategoryDir + "/" + slug + ".md"
	today := now.Format("2006-01-02")
	entry := dealEntryLine(in, today)

	existing, _ := s.ReadPage(relPath)
	if existing != nil {
		// Already filed this exact document → no-op (keeps Updated stable so a
		// re-analysis doesn't churn the index).
		if ref := strings.TrimSpace(in.SourceRef); ref != "" && strings.Contains(existing.Body, dealRefMarker(ref)) {
			return relPath, false, nil
		}
		existing.Body = appendToSection(existing.Body, dealDocsHeading, entry)
		existing.Meta.Updated = today
		if d := strings.TrimSpace(in.DueDate); d != "" {
			existing.Meta.Due = d // latest known due wins
		}
		if err := s.WritePage(relPath, existing); err != nil {
			return "", false, err
		}
		return relPath, false, nil
	}

	page := NewPage(counterparty, "프로젝트", nil)
	page.Meta.Type = "deal"
	page.Meta.Updated = today
	if d := strings.TrimSpace(in.DueDate); d != "" {
		page.Meta.Due = d
	}
	if sum := strings.TrimSpace(in.Summary); sum != "" {
		page.Meta.Summary = sum
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n## 요약\n\n", counterparty)
	if sum := strings.TrimSpace(in.Summary); sum != "" {
		b.WriteString(sum + "\n")
	} else {
		b.WriteString("_메일 첨부 문서에서 자동 생성됨_\n")
	}
	b.WriteString("\n## " + dealDocsHeading + "\n\n")
	b.WriteString(entry + "\n")
	page.Body = b.String()
	if err := s.WritePage(relPath, page); err != nil {
		return "", false, err
	}
	return relPath, true, nil
}

// dealEntryLine renders one document as a Markdown list item for the 거래 문서
// log. The SourceRef is embedded as a trailing marker so re-analysis can detect
// the document is already recorded.
func dealEntryLine(in DealPageInput, today string) string {
	date := strings.TrimSpace(in.Date)
	if date == "" {
		date = today
	}
	var parts []string
	if dt := strings.TrimSpace(in.DocType); dt != "" {
		parts = append(parts, dt)
	}
	if amt := strings.TrimSpace(in.Amount); amt != "" {
		parts = append(parts, amt)
	}
	head := strings.Join(parts, " · ")
	if head == "" {
		head = "문서"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "- %s · %s", date, head)
	if sum := strings.TrimSpace(in.Summary); sum != "" {
		b.WriteString(" — " + sum)
	}
	if items := dedupeStrings(in.Items); len(items) > 0 {
		b.WriteString(" [" + strings.Join(items, ", ") + "]")
	}
	if ref := strings.TrimSpace(in.SourceRef); ref != "" {
		b.WriteString(" " + dealRefMarker(ref))
	}
	return b.String()
}

// dealRefMarker is the inline provenance token used for idempotency. Backticked
// so it reads as code and is unlikely to collide with prose.
func dealRefMarker(ref string) string {
	return "`<" + ref + ">`"
}

// dealSlug lowercases ASCII, passes Korean/CJK through, and collapses runs of
// punctuation/whitespace to single hyphens — same shape as the person-page
// slug so deal filenames stay filesystem-safe and stable.
func dealSlug(name string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
			prevHyphen = false
		case r > 0x7F:
			b.WriteRune(r) // Korean/CJK pass-through
			prevHyphen = false
		default:
			if !prevHyphen && b.Len() > 0 {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// appendToSection appends line to the body's "## <heading>" section, creating
// the section when absent. Unlike upsertSection (which replaces), this keeps
// prior entries — the deal-document log grows over time. Other sections keep
// their order and content.
func appendToSection(body, heading, line string) string {
	preamble, sections := (&Page{Body: body}).SplitByH2()

	var b strings.Builder
	if strings.TrimSpace(preamble) != "" {
		b.WriteString(strings.TrimRight(preamble, "\n"))
		b.WriteString("\n\n")
	}
	appended := false
	for _, sec := range sections {
		content := strings.TrimRight(sec.Content, "\n")
		if strings.EqualFold(strings.TrimSpace(sec.Heading), heading) {
			if content != "" {
				content += "\n"
			}
			content += line
			appended = true
		}
		b.WriteString("## " + sec.Heading + "\n\n")
		b.WriteString(strings.TrimRight(content, "\n"))
		b.WriteString("\n\n")
	}
	if !appended {
		b.WriteString("## " + heading + "\n\n")
		b.WriteString(line)
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
