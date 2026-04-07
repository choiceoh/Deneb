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
	ID       string
	ThreadID string
	From     string
	To       string
	CC       string
	Subject  string
	Date     string
	Body     string
	Labels   []string
}

// LabelInfo describes a Gmail label.
type LabelInfo struct {
	ID   string
	Name string
	Type string // "system" or "user"
}
