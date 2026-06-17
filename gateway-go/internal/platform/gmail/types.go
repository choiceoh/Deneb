// Package gmail implements a native Go client for the Gmail REST API,
// replacing the external gog CLI dependency.
package gmail

// MessageSummary holds metadata for a message returned by list/search.
type MessageSummary struct {
	ID       string
	ThreadID string
	From     string
	Subject  string
	Date     string
	Snippet  string
	Labels   []string
}

// MessageDetail holds the full content of a single message.
type MessageDetail struct {
	ID          string
	ThreadID    string
	From        string
	To          string
	CC          string
	Subject     string
	Date        string
	Body        string
	Labels      []string
	Attachments []AttachmentInfo

	// Threading headers, used by the local archive thread-context lookup (the
	// LMTP path has no Gmail ThreadID, so the thread is reconstructed from these).
	// MessageIDHeader is the raw "<...@host>" Message-ID; References are the raw
	// ids referenced by this message (References + In-Reply-To). Empty for the
	// Gmail API path, which threads via ThreadID instead.
	MessageIDHeader string
	References      []string
}

// AttachmentInfo describes a single message attachment. The raw bytes are
// fetched on demand via Client.GetAttachment using AttachmentID.
type AttachmentInfo struct {
	Filename     string
	MimeType     string
	AttachmentID string
	Size         int
	// Truncated is true when the local LMTP parser had to cap retained bytes.
	// Gmail API attachments are fetched on demand and leave this false.
	Truncated bool
}

// LabelInfo describes a Gmail label.
type LabelInfo struct {
	ID   string
	Name string
	Type string // "system" or "user"
}
