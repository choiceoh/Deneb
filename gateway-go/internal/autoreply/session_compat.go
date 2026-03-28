// session_compat.go — temporary re-exports from the autoreply/session subpackage.
// TODO: Remove after all callers are updated to import autoreply/session directly.
package autoreply

import "github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"

// Type aliases — abort.go
type AbortMemory = session.AbortMemory

// Type aliases — abort_cutoff.go
type AbortCutoffContext = session.AbortCutoffContext
type SessionAbortCutoffEntry = session.SessionAbortCutoffEntry

// Type aliases — session_full.go
type SessionUpdate = session.SessionUpdate
type SessionForkParams = session.SessionForkParams
type TokenUsage = session.TokenUsage
type SessionRunAccounting = session.SessionRunAccounting
type SessionUsage = session.SessionUsage
type SessionHookEvent = session.SessionHookEvent
type SessionDelivery = session.SessionDelivery

// Type aliases — history.go
type HistoryEntry = session.HistoryEntry
type HistoryTracker = session.HistoryTracker

// Function re-exports — abort.go
var IsAbortTrigger = session.IsAbortTrigger
var IsAbortRequestText = session.IsAbortRequestText
var NewAbortMemory = session.NewAbortMemory

// Function re-exports — abort_cutoff.go
var ResolveAbortCutoffFromContext = session.ResolveAbortCutoffFromContext
var ReadAbortCutoffFromSessionEntry = session.ReadAbortCutoffFromSessionEntry
var HasAbortCutoff = session.HasAbortCutoff
var ApplyAbortCutoffToSessionEntry = session.ApplyAbortCutoffToSessionEntry
var ClearAbortCutoffInSession = session.ClearAbortCutoffInSession
var ShouldSkipMessageByAbortCutoff = session.ShouldSkipMessageByAbortCutoff
var ShouldPersistAbortCutoff = session.ShouldPersistAbortCutoff
var FormatTimestampWithAge = session.FormatTimestampWithAge

// Function re-exports — session_full.go
var ApplySessionUpdate = session.ApplySessionUpdate
var ForkSession = session.ForkSession
var ResetSessionModel = session.ResetSessionModel
var ResetSessionPrompt = session.ResetSessionPrompt
var EmitSessionHook = session.EmitSessionHook
var BuildSessionDelivery = session.BuildSessionDelivery

// Function re-exports — history.go
var NewHistoryTracker = session.NewHistoryTracker
var BuildHistoryContext = session.BuildHistoryContext
