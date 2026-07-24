package server

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
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

func (s *Server) evenGlanceEvents(now time.Time) []evenapi.GlanceEvent {
	from := now
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
				AllDay:  ev.AllDay,
			})
		}
	}

	if store, err := localcal.Default(); err == nil && store != nil {
		add(store.ListRange(from, to))
	}
	if client, err := calendar.DefaultClient(); err == nil && client != nil {
		if google, gerr := client.ListUpcoming(context.Background(), from, to, 20); gerr == nil {
			add(google)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

func (s *Server) evenGlanceTodos(now time.Time) []evenapi.GlanceTodo {
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

func (s *Server) evenGlanceUrgent(now time.Time) []evenapi.GlanceUrgent {
	if s == nil || s.workFeedStore == nil {
		return nil
	}
	items, _, err := s.workFeedStore.List(40, false)
	if err != nil {
		return nil
	}
	var out []evenapi.GlanceUrgent
	for _, it := range items {
		if it.Status == workfeed.StatusAcked {
			continue
		}
		if it.Priority < workfeed.PriorityHigh {
			continue
		}
		title := strings.TrimSpace(it.Title)
		if title == "" {
			title = strings.TrimSpace(it.Summary)
		}
		if title == "" {
			continue
		}
		out = append(out, evenapi.GlanceUrgent{Title: title, Priority: it.Priority})
		if len(out) >= 5 {
			break
		}
	}
	_ = now
	return out
}
