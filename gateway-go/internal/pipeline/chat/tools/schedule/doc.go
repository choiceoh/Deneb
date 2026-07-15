// Package schedule owns calendar, cron, and todo agent tools: merged calendar
// reads/writes, availability analysis, work-graph queries, persistent cron
// jobs, and the user's structured to-do list.
//
// It depends on calendar/cron/todo contracts and never imports its parent tools
// package; registration and structured-tool callers compose its public API.
package schedule
