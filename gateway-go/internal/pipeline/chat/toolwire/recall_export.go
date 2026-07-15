package toolwire

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/recall"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

func RegisterPolarisTools(registry toolport.ToolRegistrar, store *polaris.Store, localAI tools.LocalAIFunc) {
	recall.RegisterPolarisTools(registry, store, localAI)
}

func RegisterKnowledgeTool(registry toolport.ToolRegistrar, router *knowledge.Router) {
	recall.RegisterKnowledgeTool(registry, router)
}
