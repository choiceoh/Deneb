// zen_interconnect.go — Zen arch: Interconnect (data bus) for pipeline stages.
//
// CPU analogy: The interconnect (ring bus, mesh, or infinity fabric) connects
// CPU cores, caches, and memory controllers. High-bandwidth, low-latency data
// transfer between components is critical for performance — serialization and
// copying overhead on the data bus directly impacts throughput.
//
// Problem: The Deneb pipeline passes data between stages via intermediate
// variables and re-serialization. For example, system prompt is built as a
// string, then re-serialized to JSON, then Anthropic rebuilds it as content
// blocks. Tool definitions are serialized to LLM format on every run.
//
// Solution: PipelineContext is a zero-copy data bus that carries pre-computed
// results between pipeline stages without re-serialization or re-allocation.
// Each stage reads from and writes to the bus; downstream stages consume
// the pre-computed form directly.
package chat

import (
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// PipelineContext is the interconnect bus between pipeline stages.
// It carries pre-computed data forward without re-serialization.
//
// Usage pattern:
//
//	pc := NewPipelineContext()
//	// Stage 1 writes:
//	pc.SetModel(model, providerID, apiType)
//	// Stage 4 reads:
//	if pc.IsAnthropic() { ... }
//
// This replaces scattered local variables with a structured data bus,
// reducing the risk of inconsistency between stages.
type PipelineContext struct {
	// Model resolution results (stage 1).
	Model      string
	ProviderID string
	APIType    string
	Client     *llm.Client

	// Decoded message hints (stage 0).
	Decoded DecodedMessage

	// System prompt (stage 4).
	SystemPrompt json.RawMessage

	// Knowledge addition (stage 5).
	KnowledgeAddition string

	// Assembled messages (stage 5).
	Messages []llm.Message

	// Tool definitions (stage 7).
	Tools []llm.Tool
}

// NewPipelineContext creates an empty interconnect bus.
func NewPipelineContext() *PipelineContext {
	return &PipelineContext{}
}

// IsAnthropic returns true if the resolved API type is Anthropic.
// Used by multiple stages to select the correct format.
func (pc *PipelineContext) IsAnthropic() bool {
	return pc.APIType == "anthropic"
}

// AppendSystemText appends text to the system prompt on the bus.
func (pc *PipelineContext) AppendSystemText(text string) {
	pc.SystemPrompt = llm.AppendSystemText(pc.SystemPrompt, text)
}

// SetAnthropicToolCaching marks the last tool for ephemeral caching
// (Anthropic-specific optimization).
func (pc *PipelineContext) SetAnthropicToolCaching() {
	if pc.IsAnthropic() && len(pc.Tools) > 0 {
		pc.Tools[len(pc.Tools)-1].CacheControl = &llm.CacheControl{Type: "ephemeral"}
	}
}
