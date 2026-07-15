// signals_facade.go — thin re-exports of the heartbeat signal collectors that
// now live in internal/runtime/heartbeat/signals. The collectors themselves
// depend on calendar/meeting/wikiport/localtodo; hoisting them into their own
// package keeps those imports out of this package's fanout while callers
// (internal/runtime/server) keep importing only internal/runtime/heartbeat.
package heartbeat

import "github.com/choiceoh/deneb/gateway-go/internal/runtime/heartbeat/signals"

// CalendarSignalCollector maps upcoming calendar events into heartbeat signals.
var CalendarSignalCollector = signals.CalendarSignalCollector

// DealDeadlineSignalCollector maps wiki deal records into deadline signals.
var DealDeadlineSignalCollector = signals.DealDeadlineSignalCollector

// TodoDeadlineCollector maps local open-loop todos into heartbeat deadlines.
var TodoDeadlineCollector = signals.TodoDeadlineCollector

// CombineCollectors merges independent heartbeat signal snapshots.
var CombineCollectors = signals.CombineCollectors
