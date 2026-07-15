// Package aibind re-exports ai packages used by the server composition root.
// Type/var aliases only — no adapter logic.
package aibind

import (
	aiagent "github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	embedding "github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	llm "github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	localai "github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	modelrole "github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	observatory "github.com/choiceoh/deneb/gateway-go/internal/ai/observatory"
	aiprovider "github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	rerank "github.com/choiceoh/deneb/gateway-go/internal/ai/rerank"
)

// --- ai/agent ---

type (
	JobTracker     = aiagent.JobTracker
	SpilloverStore = aiagent.SpilloverStore
)

var (
	MaxResultChars    = aiagent.MaxResultChars
	NewJobTracker     = aiagent.NewJobTracker
	NewSpilloverStore = aiagent.NewSpilloverStore
)

// --- ai/embedding ---

type (
	EmbeddingClient = embedding.Client
)

var (
	NewEmbedding = embedding.New
)

// --- ai/llm ---

type (
	ChatRequest    = llm.ChatRequest
	LLMClient      = llm.Client
	Message        = llm.Message
	ThinkingConfig = llm.ThinkingConfig
)

var (
	NewTextMessage = llm.NewTextMessage
	SystemString   = llm.SystemString
)

// --- ai/localai ---

type (
	Config = localai.Config
	Hub    = localai.Hub
)

var (
	NewLocalAI = localai.New
)

// --- ai/modelrole ---

type (
	ProviderResolved  = modelrole.ProviderResolved
	ModelRoleRegistry = modelrole.Registry
	RegistryOptions   = modelrole.RegistryOptions
	Role              = modelrole.Role
	RoutingOverride   = modelrole.RoutingOverride
)

var (
	IsReasoningModel        = modelrole.IsReasoningModel
	NewRegistryWithOptions  = modelrole.NewRegistryWithOptions
	RoleCoding              = modelrole.RoleCoding
	RoleLightweight         = modelrole.RoleLightweight
	RoleMain                = modelrole.RoleMain
	RoleTiny                = modelrole.RoleTiny
	ThinkingOffDirectiveFor = modelrole.ThinkingOffDirectiveFor
)

// --- ai/observatory ---

type (
	FailureCount = observatory.FailureCount
	LoopStatus   = observatory.LoopStatus
	Report       = observatory.Report
)

var (
	Snapshot = observatory.Snapshot
)

// --- ai/provider ---

type (
	AuthManager             = aiprovider.AuthManager
	ProviderRuntimeResolver = aiprovider.ProviderRuntimeResolver
	ProviderRegistry        = aiprovider.Registry
)

var (
	NewAuthManager             = aiprovider.NewAuthManager
	NewProviderRuntimeResolver = aiprovider.NewProviderRuntimeResolver
	NewRegistry                = aiprovider.NewRegistry
)

// --- ai/rerank ---

var (
	NewFromEnv = rerank.NewFromEnv
)
