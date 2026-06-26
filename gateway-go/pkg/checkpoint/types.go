package checkpoint

import "time"

// Snapshot is one recorded state of a file prior to an agent edit. It is
// file-content only — Path/SHA256/BlobPath capture the file's bytes and nothing
// else; there is no transcript, sent-message, or side-effect rollback (see the
// package doc). Tombstone=true means the file did not exist at snapshot time;
// restoring a tombstone deletes the current file.
type Snapshot struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	PathHash  string    `json:"pathHash"`
	Seq       int       `json:"seq"`
	TakenAt   time.Time `json:"takenAt"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	Reason    string    `json:"reason"`
	Tombstone bool      `json:"tombstone,omitempty"`
	BlobPath  string    `json:"blobPath,omitempty"` // absolute path to on-disk content (empty for tombstones)
}
