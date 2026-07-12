package session

// RunMarker is the durable record of a session run that was in progress.
// Persistence belongs to runtime/sessionstore; the domain owns only the data
// needed to reconstruct an interrupted run.
type RunMarker struct {
	SessionKey     string `json:"sessionKey"`
	StartedAt      int64  `json:"startedAt"`
	LastActivityAt int64  `json:"lastActivityAt"`
	Channel        string `json:"channel,omitempty"`
	ResumeAttempts int    `json:"resumeAttempts"`
}
