package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// allPrompts returns the available MCP prompts.
func allPrompts() []Prompt {
	return []Prompt{
		{
			Name:        "deneb_system_check",
			Description: "Deneb 시스템 전체 상태 점검 (게이트웨이, 모델, 스킬, 활동 메트릭)",
		},
		{
			Name:        "deneb_memory_recall",
			Description: "특정 주제에 대한 Deneb 메모리 검색 및 요약",
			Arguments: []PromptArg{
				{Name: "query", Description: "검색할 주제/키워드", Required: true},
			},
		},
		{
			Name:        "deneb_session_review",
			Description: "최근 또는 특정 세션의 대화 내역 리뷰",
			Arguments: []PromptArg{
				{Name: "session_key", Description: "세션 키 (생략 시 최근 세션)"},
			},
		},
		{
			Name:        "deneb_daily_summary",
			Description: "오늘의 Deneb 활동 요약 (세션, 메모리 변경, 크론 작업)",
		},
		{
			Name:        "deneb_vega_deep_search",
			Description: "Vega 시맨틱 검색 + 관련 메모리를 결합한 심층 검색",
			Arguments: []PromptArg{
				{Name: "query", Description: "검색 쿼리", Required: true},
			},
		},
	}
}

// PromptManager handles prompt listing and generation.
type PromptManager struct {
	bridge  *Bridge
	prompts []Prompt
	byName  map[string]*Prompt
}

// NewPromptManager creates a prompt manager.
func NewPromptManager(bridge *Bridge) *PromptManager {
	prompts := allPrompts()
	pm := &PromptManager{
		bridge:  bridge,
		prompts: prompts,
		byName:  make(map[string]*Prompt, len(prompts)),
	}
	for i := range pm.prompts {
		pm.byName[pm.prompts[i].Name] = &pm.prompts[i]
	}
	return pm
}

// List returns all available prompts.
func (pm *PromptManager) List() []Prompt {
	return pm.prompts
}

// Get generates a prompt's messages with the given arguments.
func (pm *PromptManager) Get(ctx context.Context, name string, args map[string]string) (*PromptGetResult, error) {
	p, ok := pm.byName[name]
	if !ok {
		return nil, fmt.Errorf("unknown prompt: %s", name)
	}

	switch name {
	case "deneb_system_check":
		return pm.systemCheck(ctx)
	case "deneb_memory_recall":
		return pm.memoryRecall(ctx, args["query"])
	case "deneb_session_review":
		return pm.sessionReview(ctx, args["session_key"])
	case "deneb_daily_summary":
		return pm.dailySummary(ctx)
	case "deneb_vega_deep_search":
		return pm.vegaDeepSearch(ctx, args["query"])
	default:
		return nil, fmt.Errorf("prompt %s not implemented", p.Name)
	}
}

func (pm *PromptManager) systemCheck(ctx context.Context) (*PromptGetResult, error) {
	// Gather system data from multiple RPC calls.
	identity, _ := pm.bridge.Call(ctx, "gateway.identity.get", nil)
	activity, _ := pm.bridge.Call(ctx, "monitoring.activity", nil)
	skills, _ := pm.bridge.Call(ctx, "skills.status", nil)
	models, _ := pm.bridge.Call(ctx, "models.list", nil)

	data := fmt.Sprintf("## Deneb System Status\n\n### Gateway Identity\n```json\n%s\n```\n\n### Activity\n```json\n%s\n```\n\n### Skills\n```json\n%s\n```\n\n### Models\n```json\n%s\n```",
		prettyJSON(identity), prettyJSON(activity), prettyJSON(skills), prettyJSON(models))

	return &PromptGetResult{
		Description: "Deneb 시스템 전체 상태 점검",
		Messages: []PromptMessage{
			{Role: "user", Content: TextContent(data + "\n\n위 Deneb 시스템 상태를 분석하고 문제점이나 주의사항이 있으면 알려줘.")},
		},
	}, nil
}

func (pm *PromptManager) memoryRecall(ctx context.Context, query string) (*PromptGetResult, error) {
	if query == "" {
		return nil, fmt.Errorf("query argument is required")
	}
	params, _ := json.Marshal(map[string]any{"query": query, "limit": 20})
	results, err := pm.bridge.Call(ctx, "memory.search", params)
	if err != nil {
		return nil, fmt.Errorf("memory search: %w", err)
	}

	data := fmt.Sprintf("## Memory Search: %q\n\n```json\n%s\n```", query, prettyJSON(results))
	return &PromptGetResult{
		Description: fmt.Sprintf("'%s'에 대한 메모리 검색 결과", query),
		Messages: []PromptMessage{
			{Role: "user", Content: TextContent(data + "\n\n위 메모리 검색 결과를 분석하고 핵심 내용을 요약해줘.")},
		},
	}, nil
}

func (pm *PromptManager) sessionReview(ctx context.Context, sessionKey string) (*PromptGetResult, error) {
	var params json.RawMessage
	if sessionKey != "" {
		params, _ = json.Marshal(map[string]any{"session_key": sessionKey})
	}

	var result json.RawMessage
	var err error
	if sessionKey != "" {
		result, err = pm.bridge.Call(ctx, "sessions.preview", params)
	} else {
		result, err = pm.bridge.Call(ctx, "sessions.list", nil)
	}
	if err != nil {
		return nil, fmt.Errorf("session data: %w", err)
	}

	label := "최근 세션 목록"
	if sessionKey != "" {
		label = fmt.Sprintf("세션 %s", sessionKey)
	}

	data := fmt.Sprintf("## Session Review: %s\n\n```json\n%s\n```", label, prettyJSON(result))
	return &PromptGetResult{
		Description: label + " 리뷰",
		Messages: []PromptMessage{
			{Role: "user", Content: TextContent(data + "\n\n위 세션 데이터를 리뷰하고 주요 내용과 진행 상황을 요약해줘.")},
		},
	}, nil
}

func (pm *PromptManager) dailySummary(ctx context.Context) (*PromptGetResult, error) {
	sessions, _ := pm.bridge.Call(ctx, "sessions.list", nil)
	activity, _ := pm.bridge.Call(ctx, "monitoring.activity", nil)
	crons, _ := pm.bridge.Call(ctx, "cron.list", nil)

	data := fmt.Sprintf("## Daily Summary\n\n### Sessions\n```json\n%s\n```\n\n### Activity\n```json\n%s\n```\n\n### Cron Jobs\n```json\n%s\n```",
		prettyJSON(sessions), prettyJSON(activity), prettyJSON(crons))

	return &PromptGetResult{
		Description: "오늘의 활동 요약",
		Messages: []PromptMessage{
			{Role: "user", Content: TextContent(data + "\n\n오늘의 Deneb 활동을 요약해줘. 완료된 작업, 진행 중인 작업, 주의가 필요한 항목을 정리해줘.")},
		},
	}, nil
}

func (pm *PromptManager) vegaDeepSearch(ctx context.Context, query string) (*PromptGetResult, error) {
	if query == "" {
		return nil, fmt.Errorf("query argument is required")
	}

	vegaParams, _ := json.Marshal(map[string]any{"query": query, "limit": 10})
	memParams, _ := json.Marshal(map[string]any{"query": query, "limit": 10})

	vegaResults, _ := pm.bridge.Call(ctx, "vega.ffi.search", vegaParams)
	memResults, _ := pm.bridge.Call(ctx, "memory.search", memParams)

	data := fmt.Sprintf("## Deep Search: %q\n\n### Vega Semantic Search\n```json\n%s\n```\n\n### Memory Search\n```json\n%s\n```",
		query, prettyJSON(vegaResults), prettyJSON(memResults))

	return &PromptGetResult{
		Description: fmt.Sprintf("'%s' 심층 검색 결과", query),
		Messages: []PromptMessage{
			{Role: "user", Content: TextContent(data + "\n\n위 검색 결과를 종합 분석하고, 핵심 정보를 구조화해서 정리해줘.")},
		},
	}, nil
}

// prettyJSON formats raw JSON for readability. Falls back to raw string.
func prettyJSON(data json.RawMessage) string {
	if len(data) == 0 {
		return "(no data)"
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return string(data)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(data)
	}
	return string(out)
}
