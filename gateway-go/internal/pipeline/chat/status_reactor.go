package chat

// statusReactor is the subset of phase-status behavior the run loop drives on
// the user's triggering message (queued → preparing → recalling → thinking →
// tool → done). It was implemented by the Telegram phase-emoji reaction
// controller, retired with the Telegram bot.
//
// The seam is kept so the run pipeline stays channel-agnostic. No channel
// currently wires a reactor, so statusCtrl is always nil and the guarded calls
// throughout the run path are inert — the native client surfaces phase via
// structured WebSocket/SSE events instead of message reactions.
type statusReactor interface {
	SetQueued()
	SetPreparing()
	SetRecalling()
	SetThinking()
	SetTool(name string)
	SetCompacting()
	SetClear()
	SetDone()
	SetError()
	CloseAfterDrain()
}
