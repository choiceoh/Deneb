package chat

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"

// Type aliases — canonical definitions are in tooldeps/.

// CoreToolDeps holds all dependencies for core agent tools.
type CoreToolDeps = tooldeps.CoreToolDeps

// ProcessDeps holds dependencies for exec and process management tools.
type ProcessDeps = tooldeps.ProcessDeps

// SessionDeps holds dependencies for session management tools.
type SessionDeps = tooldeps.SessionDeps

// ChronoDeps holds dependencies for the cron scheduling tool.
type ChronoDeps = tooldeps.ChronoDeps

// WikiDeps holds dependencies for the wiki knowledge base tool.
type WikiDeps = tooldeps.WikiDeps

// ContactsDeps holds dependencies for the contacts address-book tool.
type ContactsDeps = tooldeps.ContactsDeps

// NotebookDeps holds dependencies for the notebook tool.
type NotebookDeps = tooldeps.NotebookDeps

// CalendarDeps holds dependencies for the calendar tool.
type CalendarDeps = tooldeps.CalendarDeps

// FleetDeps holds the SparkFleet base URL + token for the fleet tool.
type FleetDeps = tooldeps.FleetDeps

// CalendarReader is the read-only Google calendar slice the calendar tool uses.
type CalendarReader = tooldeps.CalendarReader

// LocalCalendar is the read/write local calendar store slice.
type LocalCalendar = tooldeps.LocalCalendar
