// Package gmail implements a native Go client for the Gmail REST API,
// replacing the external gog CLI dependency.
package gmail

// MessageSummary holds metadata for a message returned by list/search.
type MessageSummary struct {
	ID              string
	ThreadID        string
	From            string
	Subject         string
	Date            string
	Snippet         string
	Labels          []string
	Mailbox         string
	HasAttachment   bool
	AttachmentCount int
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

	// LargeAttachments are download links to files hosted by a webmail large-file
	// service (Korean groupware's 대용량첨부 widget) rather than attached as MIME
	// bytes. The LMTP parser extracts them from the HTML body; the ingest path
	// resolves the allowlisted ones to real attachment bytes (host-gated GET).
	LargeAttachments []LargeAttachmentRef

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

// LargeAttachmentRef is a download link to a "large attachment" (대용량첨부): a
// file the sender's webmail uploaded to its own file server, leaving only a
// download URL in the body instead of MIME bytes. These are candidate links
// extracted from the body; only those matching the ingest-time host allowlist
// are actually fetched (mail bodies are untrusted — the allowlist is the SSRF
// gate). Filename is a best-effort hint from the body; the download's
// Content-Disposition is authoritative.
type LargeAttachmentRef struct {
	URL      string
	Filename string
}

// LabelInfo describes a Gmail label.
type LabelInfo struct {
	ID   string
	Name string
	Type string // "system" or "user"
}
