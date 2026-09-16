package handlerminiapp

import (
	"sort"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginelive"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/routershare"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

// assembleEngineDays merges the three per-day sources — the engine's counters,
// the liveness ledger's downtime and the router's day meter — into one row per
// day, newest first, capped at engineHistoryDays. A day the engine was gone
// for entirely has no counters and no scrape, and it is exactly the day that
// must not be missing from the list; the ledger puts it there.
//
// Downtime and router numbers attach to the day's first engine row (there is
// one local engine in practice); a day without a scrape gets a row of its own.
func assembleEngineDays(
	now time.Time,
	speedRows []enginespeed.DayStat,
	downtime map[string]enginelive.DayDowntime,
	trackedSinceMs int64,
	shareRows []routershare.DayEntry,
	local, known map[string]bool,
) ([]EngineDay, EngineTotals) {
	rows := make([]EngineDay, 0, len(speedRows))
	firstOfDay := make(map[string]int)
	for _, d := range speedRows {
		if _, ok := firstOfDay[d.Day]; !ok {
			firstOfDay[d.Day] = len(rows)
		}
		rows = append(rows, engineDayFrom(d))
	}
	rowFor := func(day string) *EngineDay {
		if i, ok := firstOfDay[day]; ok {
			return &rows[i]
		}
		firstOfDay[day] = len(rows)
		rows = append(rows, EngineDay{Day: day})
		return &rows[len(rows)-1]
	}

	trackedFrom := ""
	if trackedSinceMs > 0 {
		trackedFrom = time.UnixMilli(trackedSinceMs).In(time.Local).Format("2006-01-02")
	}
	for day, d := range downtime {
		row := rowFor(day)
		row.DownSeconds, row.Outages = d.Seconds, d.Episodes
	}
	for _, e := range shareRows {
		row := rowFor(e.Day)
		row.RouterMetered = true
		switch {
		case local[e.Model]:
			row.RouterLocalRequests += e.Requests
		case known == nil || known[e.Model]:
			row.RouterRemoteRequests += e.Requests
		default:
			row.RouterUnknownRequests += e.Requests
		}
	}
	// Every day inside the tracked span is tracked, whether or not it lost
	// anything — a quiet day must read as "up all day", not "unknown".
	if trackedFrom != "" {
		for i := range rows {
			if rows[i].Day >= trackedFrom {
				rows[i].LivenessTracked = true
			}
		}
	}

	// Stable: rows of one day keep the store's order, so the day's first
	// engine row (the one carrying downtime and the router split) stays first.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Day > rows[j].Day })
	rows = limitDays(rows, engineHistoryDays)

	var summed observe.EngineDelta
	var observed float64
	total := EngineTotals{}
	days := make(map[string]bool)
	for _, d := range speedRows {
		if !inRows(rows, d.Day) {
			continue
		}
		summed = summed.Add(d.Delta)
		observed += observedSeconds(d)
		total.Restarts += d.Restarts
	}
	for _, r := range rows {
		days[r.Day] = true
		total.DownSeconds += r.DownSeconds
		total.Outages += r.Outages
		total.RouterLocalRequests += r.RouterLocalRequests
		total.RouterRemoteRequests += r.RouterRemoteRequests
		total.RouterUnknownRequests += r.RouterUnknownRequests
	}
	total = engineTotalsFrom(total, len(days), observed, summed)
	return rows, total
}

func inRows(rows []EngineDay, day string) bool {
	for _, r := range rows {
		if r.Day == day {
			return true
		}
	}
	return false
}

// limitDays keeps every row belonging to the newest n distinct days.
func limitDays(rows []EngineDay, n int) []EngineDay {
	seen, last := 0, ""
	for i, r := range rows {
		if r.Day != last {
			seen++
			last = r.Day
			if seen > n {
				return rows[:i]
			}
		}
	}
	return rows
}

// observedSeconds is how long the sampler actually watched a day: one poll
// covers one interval. It is the honest denominator for utilization — wall
// seconds in a day would count the hours the gateway was not even running.
func observedSeconds(d enginespeed.DayStat) float64 {
	if d.PollIntervalSec <= 0 || d.Polls <= 0 {
		return 0
	}
	return float64(d.Polls) * float64(d.PollIntervalSec)
}

func engineTotalsFrom(base EngineTotals, days int, observed float64, summed observe.EngineDelta) EngineTotals {
	r := summed.Rates()
	base.Days = days
	base.Requests = r.Requests
	base.PromptTokens = r.PromptTokens
	base.GeneratedTokens = r.GeneratedTokens
	base.DecodeTokensPerSec = r.DecodeTokensPerSec
	base.PrefillTokensPerSec = r.PrefillTokensPerSec
	base.MeanTtftSeconds = r.MeanTTFTSeconds
	base.MeanQueueSeconds = r.MeanQueueSeconds
	base.MeanE2eSeconds = r.MeanE2ESeconds
	base.PromptCacheHitRatio = r.PromptCacheHitRatio
	base.SpecAcceptRatio = r.SpecAcceptRatio
	base.SpecDraftTokens = r.SpecDraftTokens
	base.BusySeconds = r.BusySeconds
	base.ObservedSeconds = observed
	if observed > 0 {
		base.Utilization = r.BusySeconds / observed
	}
	return base
}

func engineDayFrom(d enginespeed.DayStat) EngineDay {
	r := d.Rates()
	observed := observedSeconds(d)
	util := 0.0
	if observed > 0 {
		util = r.BusySeconds / observed
	}
	return EngineDay{
		Day:                  d.Day,
		Model:                d.Model,
		Measured:             r.Measured(),
		DecodeTokensPerSec:   r.DecodeTokensPerSec,
		PrefillTokensPerSec:  r.PrefillTokensPerSec,
		ConcurrencyWhileBusy: r.ConcurrencyWhileBusy,
		PeakConcurrency:      d.PeakConcurrency,
		PollIntervalSec:      d.PollIntervalSec,
		Requests:             int64(r.Requests),
		PromptTokens:         int64(r.PromptTokens),
		GeneratedTokens:      int64(r.GeneratedTokens),
		MeanTtftSeconds:      r.MeanTTFTSeconds,
		MeanQueueSeconds:     r.MeanQueueSeconds,
		MeanE2eSeconds:       r.MeanE2ESeconds,
		PromptCacheHitRatio:  r.PromptCacheHitRatio,
		CachedPromptTokens:   r.CachedPromptTokens,
		SpecAcceptRatio:      r.SpecAcceptRatio,
		SpecDraftTokens:      r.SpecDraftTokens,
		BusySeconds:          r.BusySeconds,
		ObservedSeconds:      observed,
		Utilization:          util,
		Restarts:             d.Restarts,
	}
}

// engineOutagesFrom renders outages newest first, an ongoing one closed at
// now for its duration only.
func engineOutagesFrom(outages []enginelive.Outage, now time.Time) []EngineOutage {
	out := make([]EngineOutage, 0, len(outages))
	for i := len(outages) - 1; i >= 0; i-- {
		o := outages[i]
		out = append(out, EngineOutage{
			SinceMs:     o.SinceMs,
			UntilMs:     o.UntilMs,
			DurationSec: o.Duration(now.UnixMilli()).Seconds(),
			Reason:      o.Reason,
		})
	}
	return out
}

func engineInternalsFrom(in observe.EngineInternals) EngineInternals {
	return EngineInternals{
		Published:                 in.Published,
		PrefixEntries:             in.PrefixEntries,
		PrefixSnapshotsFree:       in.PrefixSnapshotsFree,
		PrefixPinnedEntries:       in.PrefixPinnedEntries,
		PrefixTierEntries:         in.PrefixTierEntries,
		KvBlocksTotal:             in.KVBlocksTotal,
		KvBlocksUsed:              in.KVBlocksUsed,
		KvBlocksCached:            in.KVBlocksCached,
		ConversationsParked:       in.ConversationsParked,
		DeviceMemoryTotalBytes:    in.DeviceMemoryTotalBytes,
		DeviceMemoryFreeBytes:     in.DeviceMemoryFreeBytes,
		DeviceMemoryReservedBytes: in.DeviceMemoryReservedBytes,
		HostMemoryAvailableBytes:  in.HostMemoryAvailableBytes,
		HandingOver:               in.HandingOver,
		Quiet:                     in.Quiet,
	}
}

func applyFleet(in *EngineInternals, f observe.EngineFleet) {
	in.FleetKnown = f.FleetKnown
	in.FleetOwner = f.Owner
	in.FleetDraining = f.Draining
	in.FleetHandedOver = f.HandedOver
	in.Served = f.Served
	in.Steps = f.Steps
	if in.ConversationsParked == 0 {
		in.ConversationsParked = f.Parked
	}
}

// routingRowsFrom classifies the meter's rows and sums the split. An entry
// the router config no longer lists is neither local nor remote: it is
// unknown, and saying "remote" for it is how a renamed local entry turned
// into cloud traffic on the month view. Without a config list (known nil)
// nothing can be told apart, and every non-local row counts remote as before.
func routingRowsFrom(models []observe.RouterModelUsage, local, known map[string]bool) (rows []EngineRoutingRow, localN, remoteN, unknownN int64) {
	rows = make([]EngineRoutingRow, 0, len(models))
	for _, m := range models {
		row := EngineRoutingRow{
			Model: m.Model, Local: local[m.Model], Known: known == nil || local[m.Model] || known[m.Model],
			Requests: m.Requests, InputTokens: m.InputTokens, OutputTokens: m.OutputTokens,
		}
		switch {
		case row.Local:
			localN += m.Requests
		case row.Known:
			remoteN += m.Requests
		default:
			unknownN += m.Requests
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Requests > rows[j].Requests })
	return rows, localN, remoteN, unknownN
}

// fillRouterToday reads today's rows out of the day meter.
func fillRouterToday(out *EngineStatusResult, now time.Time, shareRows []routershare.DayEntry, local, known map[string]bool) {
	today := now.Format("2006-01-02")
	out.RouterDay = today
	var todays []observe.RouterModelUsage
	for _, e := range shareRows {
		if e.Day != today {
			continue
		}
		todays = append(todays, observe.RouterModelUsage{
			Model: e.Model, Requests: e.Requests, InputTokens: e.InputTokens, OutputTokens: e.OutputTokens,
		})
	}
	if len(todays) == 0 {
		return
	}
	out.RouterDayMetered = true
	out.RoutingToday, out.TodayLocalRequests, out.TodayRemoteRequests, out.TodayUnknownRequests = routingRowsFrom(todays, local, known)
}

// applyRouterStatus stamps each row with the router's live state for its entry.
func applyRouterStatus(rows []EngineRoutingRow, status observe.RouterStatus) {
	by := status.ByName()
	for i := range rows {
		st, ok := by[rows[i].Model]
		if !ok {
			continue
		}
		rows[i].CircuitState = st.CircuitState
		rows[i].CircuitFailures = st.CircuitFailures
		rows[i].RetryAfterMs = st.RetryAfterMS
		rows[i].KeyHealth = st.KeyHealth
		rows[i].UpstreamMissing = st.UpstreamMissing
	}
}
