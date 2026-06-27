// deal_records.go — the typed, computable projection of filed business documents.
//
// UpsertDealPage renders each document into a prose 거래 page for reading, but
// prose cannot be summed or counted: "총 거래액", "미수 합계", "단가 50만↑ 몇 건"
// force the model to eyeball figures it routinely gets wrong. This file tees a
// typed DealRecord — the free-text 금액 parsed to a number + currency — into an
// append-only ledger so those questions become deterministic computation: the
// "executable memory" substrate (User as Code, arXiv 2606.16707) layered over
// the structured data Deneb already has in hand at write time.
package wiki

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// dealRecordsFile is the append-only typed-deal ledger, a sibling of the prose
// 거래 pages. The dot prefix + .jsonl suffix keep it out of the .md page index.
const dealRecordsFile = ".deals.jsonl"

// DealRecord is one filed business document as typed, computable fields — the
// structured counterpart of the prose entry dealEntryLine renders.
type DealRecord struct {
	Counterparty string   `json:"counterparty"`
	DocType      string   `json:"docType,omitempty"`
	AmountRaw    string   `json:"amountRaw,omitempty"`   // original free-text, always kept
	AmountValue  float64  `json:"amountValue,omitempty"` // parsed numeric; 0 when unparsed
	Currency     string   `json:"currency,omitempty"`    // "KRW"|"USD"|"EUR"|"JPY"|""
	AmountParsed bool     `json:"amountParsed"`          // false → exclude from sums
	Date         string   `json:"date,omitempty"`        // YYYY-MM-DD (or raw when unparseable)
	DueDate      string   `json:"dueDate,omitempty"`
	Items        []string `json:"items,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	SourceRef    string   `json:"sourceRef,omitempty"`
	RecordedAt   int64    `json:"recordedAt"` // unix milli
}

// dealRecordFrom builds a typed record from the write-time input, parsing the
// free-text 금액. now is injected for deterministic tests.
func dealRecordFrom(in DealPageInput, now time.Time) DealRecord {
	val, cur, ok := ParseAmount(in.Amount)
	date := strings.TrimSpace(in.Date)
	if date == "" {
		date = now.Format("2006-01-02")
	}
	return DealRecord{
		Counterparty: strings.TrimSpace(in.Counterparty),
		DocType:      strings.TrimSpace(in.DocType),
		AmountRaw:    strings.TrimSpace(in.Amount),
		AmountValue:  val,
		Currency:     cur,
		AmountParsed: ok,
		Date:         date,
		DueDate:      strings.TrimSpace(in.DueDate),
		Items:        dedupeStrings(in.Items),
		Summary:      strings.TrimSpace(in.Summary),
		SourceRef:    strings.TrimSpace(in.SourceRef),
		RecordedAt:   now.UnixMilli(),
	}
}

// appendDealRecord appends one typed record to the ledger. Best-effort by
// contract: the prose page is the source of truth, so a ledger write failure is
// returned for logging but must not fail the already-committed page write.
func (s *Store) appendDealRecord(rec DealRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	s.dealMu.Lock()
	defer s.dealMu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.dir, dealRecordsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// ListDealRecords returns every typed deal record, oldest first. A missing
// ledger yields an empty slice, not an error (no deals filed yet). Malformed
// lines are skipped — the ledger is derived, best-effort data.
func (s *Store) ListDealRecords() ([]DealRecord, error) {
	s.dealMu.Lock()
	defer s.dealMu.Unlock()
	f, err := os.Open(filepath.Join(s.dir, dealRecordsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []DealRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec DealRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

// ParseAmount parses a free-text 금액 ("5,000,000원", "$1,200", "1200달러") into a
// numeric value and an ISO-ish currency code. ok=false (value 0) when no ASCII
// numeric is present — notably spelled-out Korean numerals ("오백만원", "5천만원"),
// which are intentionally out of scope for v1 and surfaced as unparsed rather
// than guessed. Formal business documents (견적서/세금계산서/거래명세서) use digit
// grouping, the dominant case this covers.
func ParseAmount(raw string) (value float64, currency string, ok bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, "", false
	}
	currency = detectCurrency(s)
	num, trailingKorean := extractLeadingNumber(s)
	if num == "" || trailingKorean {
		return 0, currency, false
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, currency, false
	}
	return v, currency, true
}

func detectCurrency(s string) string {
	up := strings.ToUpper(s)
	switch {
	case strings.Contains(s, "₩"), strings.Contains(s, "원"), strings.Contains(up, "KRW"):
		return "KRW"
	case strings.Contains(s, "$"), strings.Contains(s, "달러"), strings.Contains(s, "불"), strings.Contains(up, "USD"):
		return "USD"
	case strings.Contains(s, "€"), strings.Contains(s, "유로"), strings.Contains(up, "EUR"):
		return "EUR"
	case strings.Contains(s, "¥"), strings.Contains(s, "엔"), strings.Contains(up, "JPY"):
		return "JPY"
	}
	return ""
}

// koreanNumeral marks runes that signal a spelled-out Korean number, so a digit
// immediately followed by one ("5천만") is treated as out-of-scope rather than
// silently truncated to the leading digit.
var koreanNumeral = map[rune]bool{
	'억': true, '만': true, '천': true, '백': true, '십': true,
	'일': true, '이': true, '삼': true, '사': true, '오': true,
	'육': true, '칠': true, '팔': true, '구': true, '영': true, '공': true,
}

// extractLeadingNumber returns the first contiguous digit run (commas dropped, a
// decimal point kept) and whether it is immediately followed by a Korean numeral
// unit. A string with no ASCII digit but Korean numerals returns ("", true) so
// the caller treats spelled-out amounts as unparsed.
func extractLeadingNumber(s string) (num string, trailingKorean bool) {
	rs := []rune(s)
	i := 0
	for i < len(rs) && !(rs[i] >= '0' && rs[i] <= '9') {
		i++
	}
	if i == len(rs) {
		for _, r := range rs {
			if koreanNumeral[r] {
				return "", true
			}
		}
		return "", false
	}
	var b strings.Builder
	for i < len(rs) {
		r := rs[i]
		switch {
		case r >= '0' && r <= '9', r == '.':
			b.WriteRune(r)
			i++
		case r == ',':
			i++
		default:
			return b.String(), koreanNumeral[r]
		}
	}
	return b.String(), false
}
