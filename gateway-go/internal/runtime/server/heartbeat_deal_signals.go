// heartbeat_deal_signals.go — feeds per-document deal payment/delivery deadlines
// from the typed deal-record ledger (wiki/deal_records.go) into the proactive
// signal engine, so the heartbeat can flag an imminent 기한 between morning letters.
//
// This is the UaC #3 slice (User as Code, arXiv 2606.16707): a deterministic
// constraint over typed state (a deal's DueDate) surfaced as a proactive alert.
// It reads the per-DOCUMENT due — finer than the page-level Meta.Due, which is
// "latest known wins" and can mask an earlier document's deadline. The signal
// engine's DeadlineWindow + the operator EscalateThreshold gate whether it fires.
package server

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// newDealDeadlineSignalCollector returns a collector mapping near-due typed deal
// records into autonomous deadline signals. Best-effort by contract: a nil store
// accessor, nil store, or read error yields an empty snapshot (no signals) so the
// heartbeat is unaffected. Returns nil when getStore is nil so callers can wire
// it unconditionally.
func newDealDeadlineSignalCollector(getStore func() *wiki.Store) func(ctx context.Context) autonomous.SignalInputs {
	if getStore == nil {
		return nil
	}
	return func(context.Context) autonomous.SignalInputs {
		in := autonomous.SignalInputs{Now: time.Now()}
		store := getStore()
		if store == nil {
			return in
		}
		recs, err := store.ListDealRecords()
		if err != nil {
			return in
		}
		in.Deadlines = dealDeadlines(recs, dentime.Location())
		return in
	}
}

// dealDeadlines projects typed deal records with a YYYY-MM-DD DueDate onto the
// engine's deadline inputs. The due time is the END of the due day in loc, so a
// document due "today" stays approaching (not already-past at midnight) until the
// day closes. Records with no/unparseable due are skipped, and identical
// (counterparty, due, amount) tuples are de-duplicated so one payment does not
// emit N copies. The engine applies its own DeadlineWindow.
func dealDeadlines(recs []wiki.DealRecord, loc *time.Location) []autonomous.DeadlineSignalInput {
	if loc == nil {
		loc = time.Local
	}
	seen := make(map[string]bool, len(recs))
	var out []autonomous.DeadlineSignalInput
	for _, r := range recs {
		due := strings.TrimSpace(r.DueDate)
		if due == "" {
			continue
		}
		day, err := time.ParseInLocation("2006-01-02", due, loc)
		if err != nil {
			continue
		}
		key := r.Counterparty + "|" + due + "|" + r.AmountRaw
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, autonomous.DeadlineSignalInput{
			Label: dealDeadlineLabel(r),
			Due:   day.Add(24*time.Hour - time.Second),
		})
	}
	return out
}

// dealDeadlineLabel renders a short human label: "<거래처> · <문서> · <금액> 기한".
func dealDeadlineLabel(r wiki.DealRecord) string {
	parts := []string{strings.TrimSpace(r.Counterparty)}
	if dt := strings.TrimSpace(r.DocType); dt != "" {
		parts = append(parts, dt)
	}
	if amt := strings.TrimSpace(r.AmountRaw); amt != "" {
		parts = append(parts, amt)
	}
	return strings.Join(parts, " · ") + " 기한"
}
