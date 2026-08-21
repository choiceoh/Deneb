package recall

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/recallops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

// Aliases so the toolwire parent can re-export Register* without importing
// knowledge/polaris/tools/toolport directly (fanout soft bar).
type (
	ToolRegistrar = toolport.ToolRegistrar
	Store         = polaris.Store
	LocalAIFunc   = recallops.LocalAIFunc
	Router        = knowledge.Router
)
