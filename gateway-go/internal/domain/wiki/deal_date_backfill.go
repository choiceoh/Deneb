// deal_date_backfill.go — rewriting the ledger's pre-existing free-text dates
// to ISO, once, in place.
//
// Filing normalizes 문서 일자 from here on (deal_date.go), but the rows already
// on disk keep whatever the source document said, and they are the rows every
// 월별 추이 has to read. This is the one-off migration, run through
// cmd/deal-date-backfill.
//
// Two properties the ledger's status as production financial data demands:
//
//   - It edits surgically. Rows are rewritten key-by-key from their original
//     bytes, in their original order, so a row that needs no change comes out
//     byte-identical and a row that does differs only in its date fields. The
//     git diff of ~/.deneb/wiki is then a readable audit of exactly what moved
//     — which a decode/re-encode round-trip through DealRecord would not give
//     (it would silently drop any field this struct does not know about and
//     reformat every number it does).
//   - It is idempotent. A second run finds the dates already ISO, changes
//     nothing, and leaves the DateRaw values the first run recorded.
package wiki

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DealDateChange is one rewritten row, for the migration report.
type DealDateChange struct {
	Line         int // 1-based line number in the ledger
	Counterparty string
	Field        string // "date" | "dueDate"
	From         string // source text
	To           string // ISO
	YearInferred bool   // year came from the filing time, not the text
}

// DealDateBackfillReport is what a run did, or would do in dry-run.
type DealDateBackfillReport struct {
	Rows        int              // rows read
	AlreadyISO  int              // rows whose date was already ISO
	Changed     []DealDateChange // rewrites, in file order
	Unresolved  []DealDateChange // left raw; To is empty, From is the text
	RowsWritten int              // rows whose bytes changed
	BackupPath  string           // written only when applied
}

// BackfillDealDates normalizes every free-text date already in the ledger,
// anchoring year-less forms to each row's own RecordedAt. With apply=false it
// reports what it would do and touches nothing. With apply=true it first copies
// the ledger to a timestamped sibling, then replaces it atomically.
func (s *Store) BackfillDealDates(apply bool) (DealDateBackfillReport, error) {
	var rep DealDateBackfillReport
	s.dealMu.Lock()
	defer s.dealMu.Unlock()

	path := filepath.Join(s.dir, dealRecordsFile)
	src, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return rep, nil // no deals filed yet
		}
		return rep, err
	}

	// Split rather than scan so the file's exact line structure — including a
	// missing final newline — survives the rewrite untouched.
	lines := bytes.Split(src, []byte("\n"))
	var out bytes.Buffer
	for i, line := range lines {
		if i > 0 {
			out.WriteByte('\n')
		}
		if len(bytes.TrimSpace(line)) == 0 {
			out.Write(line) // blank line (or the tail after a final newline)
			continue
		}
		rep.Rows++
		rewritten, changes, err := rewriteDealDateLine(line, i+1, &rep)
		if err != nil {
			// A line we cannot parse is data we do not understand;
			// ListDealRecords already skips it, and the migration must not
			// drop it either — copy it through verbatim.
			out.Write(line)
			continue
		}
		if changes > 0 {
			rep.RowsWritten++
		}
		out.Write(rewritten)
	}

	if !apply || rep.RowsWritten == 0 {
		return rep, nil
	}
	stamp := time.Now().Format("20060102-150405")
	rep.BackupPath = path + ".bak-" + stamp
	if err := os.WriteFile(rep.BackupPath, src, 0o644); err != nil {
		return rep, fmt.Errorf("backup: %w", err)
	}
	// dealMu only orders writers inside this process, and the production
	// gateway is a different one: it appends to this same file whenever a
	// document is filed. Replacing the file wholesale would drop any row it
	// appended while we were working, so compare-and-swap on the bytes we read
	// and make the operator re-run instead.
	if dealDateBackfillPreWriteHook != nil {
		dealDateBackfillPreWriteHook()
	}
	if err := requireUnchanged(path, src); err != nil {
		return rep, err
	}
	if err := writeFileAtomic(path, out.Bytes()); err != nil {
		return rep, fmt.Errorf("rewrite ledger: %w", err)
	}
	return rep, nil
}

// dealDateBackfillPreWriteHook runs just before the compare-and-swap. Tests
// use it to stand in for the production gateway appending mid-migration; it is
// nil everywhere else.
var dealDateBackfillPreWriteHook func()

// requireUnchanged fails when path no longer holds the bytes the run started
// from — another process appended to the ledger mid-migration.
func requireUnchanged(path string, src []byte) error {
	now, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("re-read ledger before write: %w", err)
	}
	if !bytes.Equal(now, src) {
		return fmt.Errorf("원장이 실행 도중 바뀜 (다른 프로세스가 append) — 아무것도 쓰지 않음, 다시 실행하세요")
	}
	return nil
}

// rewriteDealDateLine returns the row's bytes with date/dueDate normalized,
// recording what it did into rep. The returned count is how many fields moved.
func rewriteDealDateLine(line []byte, lineNo int, rep *DealDateBackfillReport) ([]byte, int, error) {
	keys, vals, err := decodeOrderedObject(line)
	if err != nil {
		return nil, 0, err
	}
	// RecordedAt is the filing time and the right anchor for a year-less date.
	var anchorMillis int64
	if raw, ok := vals["recordedAt"]; ok {
		_ = json.Unmarshal(raw, &anchorMillis)
	}
	anchor := time.UnixMilli(anchorMillis)
	if anchorMillis == 0 {
		anchor = time.Time{} // unknown filing time → year-less forms stay raw
	}
	counterparty := ""
	if raw, ok := vals["counterparty"]; ok {
		_ = json.Unmarshal(raw, &counterparty)
	}

	changed := 0
	for _, f := range []struct{ field, rawField string }{
		{"date", "dateRaw"},
		{"dueDate", "dueDateRaw"},
	} {
		encoded, ok := vals[f.field]
		if !ok {
			continue
		}
		var cur string
		if err := json.Unmarshal(encoded, &cur); err != nil {
			continue
		}
		cur = strings.TrimSpace(cur)
		if cur == "" {
			continue
		}
		if f.field == "date" && isoDate(cur) {
			rep.AlreadyISO++
			continue
		}
		iso, inferred, ok := normalizeDealDateDetail(cur, anchor)
		change := DealDateChange{
			Line: lineNo, Counterparty: counterparty, Field: f.field,
			From: cur, To: iso, YearInferred: inferred,
		}
		if !ok || iso == cur {
			if !ok {
				rep.Unresolved = append(rep.Unresolved, DealDateChange{
					Line: lineNo, Counterparty: counterparty, Field: f.field, From: cur,
				})
			}
			continue
		}
		rep.Changed = append(rep.Changed, change)
		vals[f.field] = mustJSONString(iso)
		// Keep the source text next to the field it came from, inserting the
		// raw key right after it so the row reads in the same order it always
		// did. A re-run overwrites in place rather than inserting twice.
		if _, exists := vals[f.rawField]; !exists {
			keys = insertAfter(keys, f.field, f.rawField)
		}
		vals[f.rawField] = mustJSONString(cur)
		changed++
	}
	if changed == 0 {
		return line, 0, nil // byte-identical
	}
	return encodeOrderedObject(keys, vals), changed, nil
}

// decodeOrderedObject splits a JSON object into its keys in source order and
// its values as verbatim bytes — the values are never re-encoded, which is what
// keeps untouched fields (amounts, terms, lineItems) bit-for-bit intact.
func decodeOrderedObject(line []byte) ([]string, map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, nil, fmt.Errorf("not a JSON object")
	}
	var keys []string
	vals := map[string]json.RawMessage{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := kt.(string)
		if !ok {
			return nil, nil, fmt.Errorf("non-string object key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, nil, err
		}
		if _, dup := vals[key]; !dup {
			keys = append(keys, key)
		}
		vals[key] = raw
	}
	if _, err := dec.Token(); err != nil && err != io.EOF { // closing '}'
		return nil, nil, err
	}
	return keys, vals, nil
}

// encodeOrderedObject re-emits the object in the given key order.
func encodeOrderedObject(keys []string, vals map[string]json.RawMessage) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(mustJSONString(k))
		b.WriteByte(':')
		b.Write(vals[k])
	}
	b.WriteByte('}')
	return b.Bytes()
}

// insertAfter places want directly after anchor in keys (appending if the
// anchor is absent), leaving every other position untouched.
func insertAfter(keys []string, anchor, want string) []string {
	for _, k := range keys {
		if k == want {
			return keys
		}
	}
	for i, k := range keys {
		if k == anchor {
			out := make([]string, 0, len(keys)+1)
			out = append(out, keys[:i+1]...)
			out = append(out, want)
			return append(out, keys[i+1:]...)
		}
	}
	return append(keys, want)
}

func mustJSONString(s string) json.RawMessage {
	b, err := json.Marshal(s)
	if err != nil { // unreachable: strings always marshal
		return json.RawMessage(`""`)
	}
	return b
}
