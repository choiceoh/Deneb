package chat

import chatrecall "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/recall"

// FileRecallHit is the transport-neutral file-search result used by recall preflight.
type FileRecallHit = chatrecall.FileRecallHit

// FileRecallFunc searches the on-box file store for recall evidence.
type FileRecallFunc = chatrecall.FileRecallFunc
