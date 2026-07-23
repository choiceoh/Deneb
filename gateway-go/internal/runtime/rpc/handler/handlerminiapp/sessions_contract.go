package handlerminiapp

// Wire shapes remain in the generator root while the sessions subpackage owns
// listing, transcript projection, display sanitation, and deletion behavior.

//deneb:wire
type sessionRowOut struct {
	Key         string `json:"key"`
	Kind        string `json:"kind,omitempty"`
	Status      string `json:"status,omitempty"`
	Channel     string `json:"channel,omitempty"`
	Model       string `json:"model,omitempty"`
	Label       string `json:"label,omitempty"`
	UpdatedAtMs int64  `json:"updatedAtMs,omitempty"`
	StartedAtMs *int64 `json:"startedAtMs,omitempty"`
	RuntimeMs   *int64 `json:"runtimeMs,omitempty"`
	TotalTokens *int64 `json:"totalTokens,omitempty"`
}

type transcriptAttachmentOut struct {
	Type     string `json:"type,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	URL      string `json:"url,omitempty"`
	Data     string `json:"data,omitempty"`
	Name     string `json:"name,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

//deneb:wire
type transcriptMsgOut struct {
	ID      string `json:"id,omitempty"`
	Role    string `json:"role"`
	Content string `json:"content"`
	// Reasoning is the assistant turn's chain-of-thought, extracted from the
	// persisted thinking blocks so a reloaded conversation can still show the
	// expandable reasoning block. Empty for user/tool rows and pre-reasoning history.
	Reasoning   string                    `json:"reasoning,omitempty"`
	Attachments []transcriptAttachmentOut `json:"attachments,omitempty"`
	TimestampMs int64                     `json:"timestampMs,omitempty"`
}

// Exported aliases let the owning sessions package project into the stable
// unexported wire names without duplicating the client contract.
type (
	SessionRowOut           = sessionRowOut
	TranscriptAttachmentOut = transcriptAttachmentOut
	TranscriptMsgOut        = transcriptMsgOut
)
