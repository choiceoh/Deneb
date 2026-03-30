package chat

import (
	chattools "github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

func truncate(s string, maxLen int) string { return chattools.Truncate(s, maxLen) }

func toolCron(d *ChronoDeps) ToolFunc                         { return chattools.ToolCron(d) }
func toolSessionsList(sessions *session.Manager) ToolFunc      { return chattools.ToolSessionsList(sessions) }
func toolSessionsHistory(transcript TranscriptStore) ToolFunc  { return chattools.ToolSessionsHistory(transcript) }
func toolSessionsSearch(transcript TranscriptStore) ToolFunc   { return chattools.ToolSessionsSearch(transcript) }
func toolSessionsSend(d *SessionDeps) ToolFunc                 { return chattools.ToolSessionsSend(d) }
func toolSessionsSpawn(d *SessionDeps) ToolFunc                { return chattools.ToolSessionsSpawn(d) }
