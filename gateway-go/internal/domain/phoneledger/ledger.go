// Package phoneledger owns the durable raw ledger for phone notification events.
//
// The phone-event judgment path is deliberately ephemeral ("persist nothing"):
// it decides whether to ALERT, and most events rightly end as silent NO_REPLY.
// But the silently-judged majority is exactly where the memory value lives — a
// KakaoTalk room saying "발주 다음 주로 밀렸어요" is not push-worthy (the user
// saw it) yet absolutely belongs in the project log. Without a ledger that
// content evaporated at judgment time, making the phone stream the one big
// connector with no raw dump (OpenWiki's deterministic-pull layer was the
// model here: raw capture first, synthesis separately).
//
// So: every notification/sms event is appended — redacted, bounded — to a daily
// JSONL under <state>/phone-events/, regardless of what the judgment decides.
// The noti-digest task consumes unread tails into the wiki on its own cadence,
// and files older than the retention window are pruned, so what ultimately
// persists is the wiki synthesis, not the raw feed.
package phoneledger

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

const (
	// Dirname is the ledger directory under the gateway state dir.
	Dirname = "phone-events"
	// ledgerRetentionDays bounds how long raw notification text is kept. The
	// durable form is the wiki synthesis; the raw feed is working data.
	ledgerRetentionDays = 30
	// ledgerMaxTextRunes bounds one entry's text (inbox-style notifications
	// re-carry retained message lists; a runaway payload must not bloat the
	// day file).
	ledgerMaxTextRunes = 4000
)

// Entry is one recorded notification event line.
type Entry struct {
	TS     string `json:"ts"` // RFC3339
	Type   string `json:"type"`
	Source string `json:"source"`
	Text   string `json:"text"`
}

// Ledger appends notification events to daily JSONL files. Safe for
// concurrent use within one process; nil *Ledger is a no-op sink.
type Ledger struct {
	dir    string
	logger *slog.Logger

	mu         sync.Mutex
	prunedDate string // YYYY-MM-DD the retention prune last ran for
}

// New creates a ledger rooted at dir. Empty dir disables recording
// (returns nil, which every method tolerates).
func New(dir string, logger *slog.Logger) *Ledger {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Ledger{dir: dir, logger: logger}
}

// otpKeywordRE matches the words that mark a one-time-code / security
// notification (Korean + English). ledgerMaskRE masks the accompanying short
// numeric code so an OTP that slips past the keyword gate is not stored raw.
var (
	otpKeywordRE = regexp.MustCompile(`(?i)인증\s?번호|인증\s?코드|확인\s?코드|보안\s?코드|일회용\s?비밀번호|verification code|one[- ]?time|\bOTP\b|passcode`)
	// A 4–8 digit run bounded by non-digits — the shape of a verification code.
	ledgerCodeRE = regexp.MustCompile(`(^|[^0-9])([0-9]{4,8})([^0-9]|$)`)
)

// isSensitiveNotification reports whether text looks like a one-time-code or
// security-code notification that must not be persisted to the 30-day ledger.
// The append now runs before the tiny push-worthiness gate, so an OTP SMS or
// banking notification the on-device blocklist missed would otherwise land
// raw; the redactor masks known secret patterns but not bare numeric OTPs.
// Conservative by design: an OTP keyword AND a short numeric code together.
func isSensitiveNotification(text string) bool {
	return otpKeywordRE.MatchString(text) && ledgerCodeRE.MatchString(text)
}

// Append records one event. Failures are logged and swallowed — the ledger
// must never break the judgment path it shadows.
func (l *Ledger) Append(eventType, source, text string) {
	if l == nil {
		return
	}
	// Never persist one-time-codes / security codes to the raw ledger. The
	// alert path still judges them (they just aren't remembered).
	if isSensitiveNotification(text) {
		return
	}
	// Deneb-canonical clock: day-file naming and entry timestamps are
	// human-facing wiki-adjacent dates (the digest prompt shows them).
	now := dentime.Now()
	entry := Entry{
		TS:     now.Format(time.RFC3339),
		Type:   strings.TrimSpace(eventType),
		Source: strings.TrimSpace(source),
		// Defense in depth: the same redaction the judgment transcript path
		// applies — a notification carrying a secret must not persist it.
		Text: redact.String(truncateLedgerText(text)),
	}
	line, err := json.Marshal(entry)
	if err != nil {
		l.logger.Warn("phone-event ledger: marshal failed", "error", err)
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		l.logger.Warn("phone-event ledger: mkdir failed", "error", err)
		return
	}
	day := now.Format("2006-01-02")
	f, err := os.OpenFile(filepath.Join(l.dir, day+".jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		l.logger.Warn("phone-event ledger: open failed", "error", err)
		return
	}
	_, werr := f.Write(append(line, '\n'))
	cerr := f.Close()
	if werr != nil || cerr != nil {
		l.logger.Warn("phone-event ledger: write failed", "writeErr", werr, "closeErr", cerr)
	}

	// Retention prune, once per day per process — cheap and off the hot path
	// enough (a directory listing) to piggyback on the first append of a day.
	if l.prunedDate != day {
		l.prunedDate = day
		l.pruneLocked(now)
	}
}

// pruneLocked removes day files older than the retention window. Caller holds mu.
func (l *Ledger) pruneLocked(now time.Time) {
	cutoff := now.AddDate(0, 0, -ledgerRetentionDays).Format("2006-01-02")
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") || len(name) != len("2006-01-02.jsonl") {
			continue
		}
		if day := strings.TrimSuffix(name, ".jsonl"); day < cutoff {
			if rerr := os.Remove(filepath.Join(l.dir, name)); rerr == nil {
				l.logger.Info("phone-event ledger: pruned", "file", name)
			}
		}
	}
}

func truncateLedgerText(s string) string {
	r := []rune(s)
	if len(r) <= ledgerMaxTextRunes {
		return s
	}
	return string(r[:ledgerMaxTextRunes]) + " (이하 생략)"
}

// Tail is unconsumed ledger content plus the offsets to commit once the
// consumer has durably used it.
type Tail struct {
	// Entries in file order (oldest file first, in-file order preserved).
	Entries []Entry
	// NextOffsets is the per-file byte offset AFTER the returned entries —
	// commit these only when consumption succeeded.
	NextOffsets map[string]int64
	// Truncated reports the budget cut in — more unconsumed content remains.
	Truncated bool
}

// ReadTail returns unconsumed entries beyond the given per-file byte
// offsets, bounded by budgetRunes of entry text. Reading stops at whole-line
// boundaries so committed offsets always land between records. Files no
// longer on disk are dropped from NextOffsets (retention prune).
func ReadTail(dir string, offsets map[string]int64, budgetRunes int) (*Tail, error) {
	tail := &Tail{NextOffsets: map[string]int64{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return tail, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files) // day-named files sort chronologically

	budget := budgetRunes
	for _, name := range files {
		path := filepath.Join(dir, name)
		offset := offsets[name]
		next, entriesRead, truncated, rerr := readLedgerFileTail(path, offset, &budget)
		if rerr != nil {
			// Skip an unreadable file this round; keep its old offset.
			tail.NextOffsets[name] = offset
			continue
		}
		tail.Entries = append(tail.Entries, entriesRead...)
		tail.NextOffsets[name] = next
		if truncated {
			tail.Truncated = true
			// Carry forward untouched offsets for the remaining files.
			for _, rest := range files {
				if _, seen := tail.NextOffsets[rest]; !seen {
					tail.NextOffsets[rest] = offsets[rest]
				}
			}
			break
		}
	}
	return tail, nil
}

func readLedgerFileTail(path string, offset int64, budget *int) (int64, []Entry, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return offset, nil, false, err
	}
	defer f.Close()
	if offset > 0 {
		if _, serr := f.Seek(offset, 0); serr != nil {
			return offset, nil, false, serr
		}
	}
	var out []Entry
	next := offset
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		lineLen := int64(len(line)) + 1 // trailing \n
		var entry Entry
		if json.Unmarshal(line, &entry) != nil || strings.TrimSpace(entry.Text) == "" {
			next += lineLen // malformed/empty line: consume and move on
			continue
		}
		cost := len([]rune(entry.Text))
		if cost > *budget {
			return next, out, true, nil // budget exhausted — stop BEFORE this line
		}
		*budget -= cost
		next += lineLen
		out = append(out, entry)
	}
	if serr := sc.Err(); serr != nil {
		// Partial read is still usable; offsets only cover fully scanned lines.
		return next, out, false, nil //nolint:nilerr // tolerated: consume what was read
	}
	return next, out, false, nil
}
