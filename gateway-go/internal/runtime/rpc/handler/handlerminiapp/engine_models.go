package handlerminiapp

import (
	"sort"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
)

// EngineModelRow is one model the engine served inside the history window, so
// the app can show one model's statistics at a time. The engine labels its
// series by engine, not by model: on 2026-09-19 a day of GLM-5.3 with three
// Qwen3.8 windows read as one Qwen3.8 day until the store keyed rows by model.
//
//deneb:wire
type EngineModelRow struct {
	Model string `json:"model"`
	// Current is the model the engine answers as now — or, while it cannot be
	// asked, the model its last scrape named.
	Current bool `json:"current"`
	// Days is how many days of the window have a row for this model; Requests
	// is their sum, and LastDay the newest of them.
	Days     int    `json:"days"`
	Requests int64  `json:"requests"`
	LastDay  string `json:"lastDay,omitempty"`
}

// engineModelRows lists the window's models: the current one first, then the
// most recently served. The current model is listed even with no row yet (a
// switch minutes ago has nothing measured), so the app can always select it.
func engineModelRows(rows []enginespeed.DayStat, current string) []EngineModelRow {
	byModel := map[string]*EngineModelRow{}
	var order []string
	for _, d := range rows {
		if d.Model == "" {
			continue
		}
		m := byModel[d.Model]
		if m == nil {
			m = &EngineModelRow{Model: d.Model}
			byModel[d.Model] = m
			order = append(order, d.Model)
		}
		m.Days++
		m.Requests += int64(d.Rates().Requests)
		if d.Day > m.LastDay {
			m.LastDay = d.Day
		}
	}
	if current != "" && byModel[current] == nil {
		byModel[current] = &EngineModelRow{Model: current}
		order = append(order, current)
	}
	out := make([]EngineModelRow, 0, len(order))
	for _, name := range order {
		m := *byModel[name]
		m.Current = name == current
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Current != out[j].Current {
			return out[i].Current
		}
		if out[i].LastDay != out[j].LastDay {
			return out[i].LastDay > out[j].LastDay
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// pickEngineModel resolves which model's statistics a status call shows: the
// requested one when the window has it, else the current model, else the one
// served most recently. Empty only when no row names a model at all — an
// engine that never said what it serves, whose rows are then shown unfiltered.
func pickEngineModel(requested string, models []EngineModelRow) string {
	for _, m := range models {
		if requested != "" && m.Model == requested {
			return requested
		}
	}
	if len(models) > 0 {
		return models[0].Model // current first, then most recent
	}
	return ""
}

// rowsOfModel keeps the rows of one model, and the days on which only other
// models were measured: those days belong to the other models' views, so the
// current model's view does not list them as unmeasured days of its own.
func rowsOfModel(rows []enginespeed.DayStat, model string) (kept []enginespeed.DayStat, otherDays map[string]bool) {
	if model == "" {
		return rows, nil
	}
	mine := map[string]bool{}
	otherDays = map[string]bool{}
	for _, d := range rows {
		if d.Model == model {
			kept = append(kept, d)
			mine[d.Day] = true
		} else {
			otherDays[d.Day] = true
		}
	}
	for day := range mine {
		delete(otherDays, day)
	}
	return kept, otherDays
}

// diagnosticsOfModel keeps one model's diagnostic intervals. The summary only
// ever averaged one runtime; the timeline now also stops drawing another
// model's points into this one's trend.
func diagnosticsOfModel(rows []enginespeed.DiagnosticInterval, model string) []enginespeed.DiagnosticInterval {
	if model == "" {
		return rows
	}
	kept := rows[:0:0]
	for _, row := range rows {
		if row.Model == model {
			kept = append(kept, row)
		}
	}
	return kept
}
