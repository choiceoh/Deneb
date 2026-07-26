package server

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calwrite"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/evenapi"
)

// evenGlanceSources builds structured glance providers from live stores.
// Missing stores degrade quietly — FormatGlance falls back to calm empty copy.
func (s *Server) evenGlanceSources() evenapi.GlanceSources {
	return evenapi.GlanceSources{
		Events: s.evenGlanceEvents,
		Todos:  s.evenGlanceTodos,
		Urgent: s.evenGlanceUrgent,
	}
}

func (s *Server) evenGlanceEvents(ctx context.Context, now time.Time) []evenapi.GlanceEvent {
	// Look back a few hours so in-progress meetings still appear as "지금".
	from := now.Add(-6 * time.Hour)
	to := now.Add(48 * time.Hour)
	seen := map[string]struct{}{}
	var out []evenapi.GlanceEvent

	add := func(evs []calendar.Event) {
		for _, ev := range evs {
			key := ev.ID
			if key == "" {
				key = ev.Summary + "|" + ev.Start.Format(time.RFC3339)
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, evenapi.GlanceEvent{
				Summary: ev.Summary,
				Start:   ev.Start,
				End:     ev.End,
				AllDay:  ev.AllDay,
			})
		}
	}

	if store, err := localcal.Default(); err == nil && store != nil {
		add(store.ListRange(from, to))
	}
	if client, err := calendar.DefaultClient(); err == nil && client != nil {
		if google, gerr := client.ListUpcoming(ctx, from, to, 20); gerr == nil {
			add(dropMirroredGlanceEvents(google))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// dropMirroredGlanceEvents removes the Google copies of events Deneb itself
// wrote there. The `seen` dedup above cannot catch them: a mirrored pair has a
// LOCAL id on one side and a GOOGLE id on the other, so both survive and the
// glasses show one meeting twice. The miniapp calendar list solves this with
// dropMirrored; the HUD needs the same filter against the same id map.
// No-op when the write mirror is off (the syncer factory fails) — then no Google
// event is a mirror of anything.
func dropMirroredGlanceEvents(events []calendar.Event) []calendar.Event {
	syncer, err := calwrite.DefaultSyncer(nil)
	if err != nil || syncer == nil {
		return events
	}
	return filterMirroredEvents(events, syncer.MirroredGoogleIDs())
}

// filterMirroredEvents is the pure half of dropMirroredGlanceEvents (the id map
// comes from the process-wide syncer, so the filter is split out to be testable).
func filterMirroredEvents(events []calendar.Event, mirrored map[string]struct{}) []calendar.Event {
	if len(mirrored) == 0 {
		return events
	}
	out := make([]calendar.Event, 0, len(events))
	for _, ev := range events {
		if _, isMirror := mirrored[ev.ID]; isMirror {
			continue
		}
		out = append(out, ev)
	}
	return out
}

func (s *Server) evenGlanceTodos(_ context.Context, now time.Time) []evenapi.GlanceTodo {
	store, err := localtodo.Default()
	if err != nil || store == nil {
		return nil
	}
	var out []evenapi.GlanceTodo
	for _, td := range store.List() {
		if td.Done {
			continue
		}
		out = append(out, evenapi.GlanceTodo{
			Title:     td.Title,
			Due:       td.Due,
			DueAllDay: td.DueAllDay,
		})
	}
	_ = now
	return out
}

func (s *Server) evenGlanceUrgent(_ context.Context, now time.Time) []evenapi.GlanceUrgent {
	// Glance HUD: unread + unacked workfeed cards at PriorityNormal+.
	// ReadAtMs>0 means the operator already opened the card in the native feed.
	if s == nil || s.workFeedStore == nil {
		return nil
	}
	items, _, err := s.workFeedStore.List(80, false)
	if err != nil {
		return nil
	}
	var out []evenapi.GlanceUrgent
	for _, it := range items {
		if it.Status == workfeed.StatusAcked {
			continue
		}
		if it.ReadAtMs > 0 {
			continue
		}
		if it.Priority > 0 && it.Priority < workfeed.PriorityNormal {
			continue
		}
		title := strings.TrimSpace(it.Title)
		summary := strings.TrimSpace(it.Summary)
		if title == "" {
			title = summary
			summary = ""
		}
		if title == "" {
			continue
		}
		preview := summary
		if preview == title {
			preview = ""
		}
		created := time.Time{}
		if it.CreatedAtMs > 0 {
			created = time.UnixMilli(it.CreatedAtMs).In(now.Location())
		}
		prio := it.Priority
		if prio == 0 {
			prio = workfeed.PriorityNormal
		}
		body := strings.TrimSpace(it.Body)
		if body == "" {
			body = preview
		}
		out = append(out, evenapi.GlanceUrgent{
			ID: it.ID, Title: title, Preview: preview, Body: body, Priority: prio, CreatedAt: created,
		})
		if len(out) >= 12 {
			break
		}
	}
	return out
}

// evenAckAlert lets the glasses clear one workfeed card.
//
// The store is resolved when the ACK ARRIVES, never when the route is
// registered. workFeedStore is populated during registerEarlyMethods, so a
// closure that captured it at buildMux time would capture nil — permanently,
// and silently, long after the store came up. (It did worse than that here: it
// panicked, because the field is not even reachable that early.)
//
// Reporting unavailability as an error rather than a nil hook keeps the HUD
// honest — a glass that says "확인함" while the alert stays is worse than one
// that says it could not.
func (s *Server) evenAckAlert() func(id string) error {
	return func(id string) error {
		if s == nil || s.workFeedStore == nil {
			return evenapi.ErrAckUnavailable
		}
		_, err := s.workFeedStore.Ack(id)
		return err
	}
}
