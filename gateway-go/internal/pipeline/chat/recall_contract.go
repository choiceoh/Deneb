package chat

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/leafbind"

// FileRecallHit is the transport-neutral file-search result used by recall preflight.
type FileRecallHit = leafbind.FileRecallHit

// FileRecallFunc searches the on-box file store for recall evidence.
type FileRecallFunc = leafbind.FileRecallFunc
