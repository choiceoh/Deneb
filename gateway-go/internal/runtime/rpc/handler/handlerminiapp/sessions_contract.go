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
	Pinned      bool   `json:"pinned,omitempty"`
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
	// ToolTrace lists the tool calls this assistant message issued, rebuilt
	// from the stored tool_use/tool_result pairs with the same digests the
	// live stream showed — so a restored conversation keeps its tool chips
	// (the display strips drop the raw blocks themselves).
	ToolTrace []transcriptToolTraceOut `json:"toolTrace,omitempty"`
}

// transcriptToolTraceOut is one completed tool call on a restored assistant
// message, in the live chip's vocabulary (toolport.ToolTraceItem).
//
//deneb:wire
type transcriptToolTraceOut struct {
	Tool string `json:"tool"`
	// Detail is the started-frame human hint (command, query, file name).
	Detail string `json:"detail,omitempty"`
	// Summary/Preview are the gateway-owned result digests.
	Summary string `json:"summary,omitempty"`
	Preview string `json:"preview,omitempty"`
	IsError bool   `json:"isError,omitempty"`
}

// sessionSearchHitOut is one conversation that matched a user drawer search.
//
//deneb:wire
type sessionSearchHitOut struct {
	SessionKey string `json:"sessionKey"`
	Snippet    string `json:"snippet,omitempty"`
	Label      string `json:"label,omitempty"`
}

// sessionSearchResult is the miniapp.sessions.search payload.
//
//deneb:wire
type sessionSearchResult struct {
	Hits []sessionSearchHitOut `json:"hits"`
}

// sessionFocusResult is the miniapp.sessions.focus payload.
//
//deneb:wire
type sessionFocusResult struct {
	SessionKey string `json:"sessionKey"`
}

// Exported aliases let the owning sessions package project into the stable
// unexported wire names without duplicating the client contract.
type (
	SessionRowOut           = sessionRowOut
	TranscriptAttachmentOut = transcriptAttachmentOut
	TranscriptMsgOut        = transcriptMsgOut
	TranscriptToolTraceOut  = transcriptToolTraceOut
	SessionSearchHitOut     = sessionSearchHitOut
	SessionSearchResult     = sessionSearchResult
	SessionFocusResult      = sessionFocusResult
)
