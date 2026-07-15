package toolwire

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/recall"
)

func RegisterPolarisTools(registry recall.ToolRegistrar, store *recall.Store, localAI recall.LocalAIFunc) {
	recall.RegisterPolarisTools(registry, store, localAI)
}

func RegisterKnowledgeTool(registry recall.ToolRegistrar, router *recall.Router) {
	recall.RegisterKnowledgeTool(registry, router)
}
