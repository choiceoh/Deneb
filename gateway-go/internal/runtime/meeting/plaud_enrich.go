// plaud_enrich.go — due extraction, calendar link, prior-meeting continuity,
// related-project fallback, and transcript spill helpers for Plaud ingest.
package meeting

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

var plaudDueDateRE = regexp.MustCompile(`\b(20\d{2}-\d{2}-\d{2})\b`)

const (
	plaudCalendarMatchWindow  = 2 * time.Hour
	plaudPriorMeetingMaxRunes = 1_200
	plaudMaxFailures          = 3
	plaudFirstTickDelay       = 90 * time.Second
	plaudListToolAlt          = "plaud_search_recordings"
)

// extractEarliestDue returns the earliest YYYY-MM-DD found in ## 액션 아이템
// (or the whole report if that section is missing). Empty when none.
func extractEarliestDue(report string) string {
	sec := sectionAfterHeading(report, "액션")
	if sec == "" {
		sec = report
	}
	var earliest string
	for _, m := range plaudDueDateRE.FindAllString(sec, -1) {
		if _, err := time.Parse("2006-01-02", m); err != nil {
			continue
		}
		if earliest == "" || m < earliest {
			earliest = m
		}
	}
	return earliest
}

// relatedProjectsOrFallback uses the model trailer when present; otherwise the
// ranked mention list (anti-hallucination: only candidate paths).
func relatedProjectsOrFallback(report string, cands []mailanalysis.ProjectCandidate, recordingName, transcript string) (string, []string) {
	body, related := splitRelatedProjects(report, cands)
	if len(related) > 0 {
		return body, related
	}
	mentioned := RankMentionedProjects(recordingName, transcript, cands, plaudRelatedProjectCap)
	if len(mentioned) == 0 {
		return body, nil
	}
	out := make([]string, 0, len(mentioned))
	for _, m := range mentioned {
		out = append(out, m.Path)
	}
	return body, out
}

// matchCalendarEvent finds a calendar event whose window overlaps the recording
// start (±plaudCalendarMatchWindow) and whose summary shares a token with the
// recording name. Returns nil when no confident match.
func matchCalendarEvent(f plaudFile, events []calendar.Event) *calendar.Event {
	if f.StartAt.IsZero() || len(events) == 0 {
		return nil
	}
	nameTokens := calendarMatchTokens(f.Name)
	var best *calendar.Event
	bestScore := 0
	for i := range events {
		ev := &events[i]
		if ev.Status == "cancelled" || ev.AllDay {
			continue
		}
		delta := f.StartAt.Sub(ev.Start)
		if delta < 0 {
			delta = -delta
		}
		inside := !f.StartAt.Before(ev.Start.Add(-plaudCalendarMatchWindow)) &&
			!f.StartAt.After(ev.End.Add(plaudCalendarMatchWindow))
		if !inside && delta > plaudCalendarMatchWindow {
			continue
		}
		score := overlapTokenScore(nameTokens, calendarMatchTokens(ev.Summary))
		if score == 0 && delta <= 30*time.Minute {
			score = 1 // time-only weak match
		}
		if score > bestScore {
			bestScore = score
			best = ev
		}
	}
	if bestScore < 1 {
		return nil
	}
	return best
}

func calendarMatchTokens(s string) []string {
	var out []string
	for _, part := range splitHintTokens(s) {
		if utf8.RuneCountInString(part) < 2 {
			continue
		}
		out = append(out, strings.ToLower(part))
	}
	return out
}

func overlapTokenScore(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, t := range b {
		set[t] = true
	}
	score := 0
	for _, t := range a {
		if set[t] {
			score += utf8.RuneCountInString(t)
		}
	}
	return score
}

// formatPriorMeetingBlock renders a short continuity cue for synthesis.
func formatPriorMeetingBlock(title, body string) string {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" && body == "" {
		return ""
	}
	excerpt := textutil.TruncateRunes(body, plaudPriorMeetingMaxRunes, "…")
	var b strings.Builder
	b.WriteString("# 지난 회의 대비\n\n")
	if title != "" {
		fmt.Fprintf(&b, "직전 회의록: %s\n\n", title)
	}
	b.WriteString(excerpt)
	return b.String()
}

// writeTranscriptSpill persists the full transcript next to Plaud state.
// Returns the absolute spill path (empty on failure).
func writeTranscriptSpill(statePath, recordingID, transcript string) string {
	statePath = strings.TrimSpace(statePath)
	recordingID = strings.TrimSpace(recordingID)
	if statePath == "" || recordingID == "" || strings.TrimSpace(transcript) == "" {
		return ""
	}
	if strings.Contains(recordingID, "..") || strings.ContainsAny(recordingID, `/\`) {
		return ""
	}
	dir := filepath.Join(filepath.Dir(statePath), "plaud-transcripts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	path := filepath.Join(dir, recordingID+".txt")
	if err := atomicfile.WriteFile(path, []byte(transcript), &atomicfile.Options{Perm: 0o600}); err != nil {
		return ""
	}
	return path
}
