package handlerminiapp

import "time"

// Mail wire shapes remain in handlerminiapp so Kotlin/TypeScript generation
// keeps one stable source. Marked types feed the native-client generator.
// list_recent returns
// {messages: []MailRowOut, nextPageToken}; get returns MailMessageOut.

//deneb:wire
type MailRowOut struct {
	ID              string   `json:"id"`
	ThreadID        string   `json:"threadId"`
	From            string   `json:"from"`
	Subject         string   `json:"subject"`
	Snippet         string   `json:"snippet"`
	Date            string   `json:"date"`
	IsUnread        bool     `json:"isUnread"`
	Labels          []string `json:"labels"`
	Mailbox         string   `json:"mailbox,omitempty"`
	HasAttachment   bool     `json:"hasAttachment,omitempty"`
	AttachmentCount int      `json:"attachmentCount,omitempty"`
	// Priority is the glanceable heuristic tier ("urgent"/"attention",
	// empty for routine mail) computed at list time by domain/mailpriority;
	// PriorityHint names the strongest signals (e.g. "낙찰 · 마감 표현").
	Priority     string `json:"priority,omitempty"`
	PriorityHint string `json:"priorityHint,omitempty"`

	AnalysisStatus        string       `json:"analysisStatus,omitempty"`
	AnalysisQuality       string       `json:"analysisQuality,omitempty"`
	FeedStatus            string       `json:"feedStatus,omitempty"`
	CalendarProposalCount int          `json:"calendarProposalCount,omitempty"`
	TodoCount             int          `json:"todoCount,omitempty"`
	WorkStateHint         string       `json:"workStateHint,omitempty"`
	RelatedProjects       []ProjectRef `json:"relatedProjects,omitempty"`
}

// MailAttachmentOut describes one attachment in the mail detail wire response.
type MailAttachmentOut struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mimeType"`
	Size      int    `json:"size"`
	Truncated bool   `json:"truncated,omitempty"`
}

//deneb:wire
type MailMessageOut struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
	From     string `json:"from"`
	To       string `json:"to"`
	CC       string `json:"cc,omitempty"`
	Subject  string `json:"subject"`
	Date     string `json:"date"`
	// IsUnread mirrors the list row's flag (derived from the UNREAD label) so the
	// detail view is self-sufficient: clients auto-mark-read off this when a mail is
	// opened directly (search, work feed, notification) with no inbox list row to
	// read it from. Without it, only inbox-list opens marked read.
	IsUnread             bool                `json:"isUnread"`
	Body                 string              `json:"body"`
	BodyTotal            int                 `json:"bodyTotal"`
	RawBody              string              `json:"rawBody,omitempty"`
	RawBodyTotal         int                 `json:"rawBodyTotal,omitempty"`
	BodyCleaned          bool                `json:"bodyCleaned,omitempty"`
	BodyHiddenBlockCount int                 `json:"bodyHiddenBlockCount,omitempty"`
	BodyHiddenLineCount  int                 `json:"bodyHiddenLineCount,omitempty"`
	Labels               []string            `json:"labels"`
	Attachments          []MailAttachmentOut `json:"attachments"`

	AnalysisStatus        string       `json:"analysisStatus,omitempty"`
	AnalysisQuality       string       `json:"analysisQuality,omitempty"`
	FeedStatus            string       `json:"feedStatus,omitempty"`
	CalendarProposalCount int          `json:"calendarProposalCount,omitempty"`
	TodoCount             int          `json:"todoCount,omitempty"`
	WorkStateHint         string       `json:"workStateHint,omitempty"`
	RelatedProjects       []ProjectRef `json:"relatedProjects,omitempty"`
}

//deneb:wire
type MailNativeStatusOut struct {
	Source         string                 `json:"source"`
	Available      bool                   `json:"available"`
	OfflineCapable bool                   `json:"offlineCapable"`
	Mailboxes      []MailNativeMailboxOut `json:"mailboxes"`
	Overlay        MailNativeOverlayOut   `json:"overlay"`
	Pipeline       MailNativePipelineOut  `json:"pipeline"`
	GeneratedAt    string                 `json:"generatedAt,omitempty"`
	Error          string                 `json:"error,omitempty"`
}

// MailNativeMailboxOut summarizes one locally archived mailbox.
type MailNativeMailboxOut struct {
	Name              string `json:"name"`
	Total             int    `json:"total"`
	Unread            int    `json:"unread"`
	LocallyRead       int    `json:"locallyRead"`
	LocallyArchived   int    `json:"locallyArchived"`
	LocallyTrashed    int    `json:"locallyTrashed"`
	LatestUID         string `json:"latestUid,omitempty"`
	AttachmentCapable bool   `json:"attachmentCapable"`
}

// MailNativeOverlayOut summarizes local read/archive/trash overlays.
type MailNativeOverlayOut struct {
	Messages int `json:"messages"`
	Read     int `json:"read"`
	Archived int `json:"archived"`
	Trashed  int `json:"trashed"`
}

// MailNativePipelineOut summarizes native mail analysis workflow state.
type MailNativePipelineOut struct {
	Messages           int    `json:"messages"`
	Analyzed           int    `json:"analyzed"`
	Analyzing          int    `json:"analyzing"`
	Failed             int    `json:"failed"`
	FeedCreated        int    `json:"feedCreated"`
	FeedMissing        int    `json:"feedMissing"`
	CalendarCandidates int    `json:"calendarCandidates"`
	TodoCandidates     int    `json:"todoCandidates"`
	UpdatedAt          string `json:"updatedAt,omitempty"`
	Error              string `json:"error,omitempty"`
}

// ProjectRef is a related project wiki page surfaced to the Mini App: the
// path plus enriched title/summary so the client renders a chip without a
// second round trip.
//
//deneb:wire
type ProjectRef struct {
	Path    string `json:"path"`
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// MailAnalysisOut is the wire shape for a Gmail AI-analysis result
// (miniapp.mail.analyze). Marked for Kotlin codegen so the native client's
// analysis card shares it; the cached endpoint returns a subset of this shape.
//
//deneb:wire
type MailAnalysisOut struct {
	ID              string       `json:"id"`
	Subject         string       `json:"subject,omitempty"`
	From            string       `json:"from,omitempty"`
	Date            string       `json:"date,omitempty"`
	Analysis        string       `json:"analysis"`
	RelatedProjects []ProjectRef `json:"relatedProjects,omitempty"`
	DurationMs      int64        `json:"durationMs"`
	Cached          bool         `json:"cached"`
	CreatedAt       time.Time    `json:"createdAt"`

	AnalysisStatus        string `json:"analysisStatus,omitempty"`
	AnalysisQuality       string `json:"analysisQuality,omitempty"`
	FeedStatus            string `json:"feedStatus,omitempty"`
	CalendarProposalCount int    `json:"calendarProposalCount,omitempty"`
	TodoCount             int    `json:"todoCount,omitempty"`
	WorkStateHint         string `json:"workStateHint,omitempty"`
}

// SenderWikiHitOut and SenderRecentOut are wire shapes for the sender-context
// card (miniapp.mail.sender_context), marked for Kotlin codegen so the native
// client shares them. The envelope itself stays handler-local (it is cached as
// `any`), but its row/sub types are generated.

//deneb:wire
type SenderWikiHitOut struct {
	Path     string `json:"path"`
	Title    string `json:"title,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Category string `json:"category,omitempty"`
}

//deneb:wire
type SenderRecentOut struct {
	Count          int    `json:"count"`
	LastReceivedAt string `json:"lastReceivedAt,omitempty"` // ISO 8601
	WindowDays     int    `json:"windowDays"`
	// Truncated is true when Count equals the per-request cap — there could be
	// more matching messages than reported. UI renders "5+" instead of "5".
	Truncated bool `json:"truncated,omitempty"`
}

// QATurn is one prior question/answer turn in a mail Q&A thread. The client
// accumulates these and re-sends them so the backend stays stateless and the
// Q&A never touches the main session transcript.
//
//deneb:wire
type QATurn struct {
	Q string `json:"q"`
	A string `json:"a"`
}
