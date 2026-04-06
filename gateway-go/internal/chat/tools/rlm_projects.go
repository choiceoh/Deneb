package tools

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
)

// ToolProjectsList returns a tool that lists projects with metadata only.
// Vega backend removed — returns unavailable message.
func ToolProjectsList(_ *toolctx.VegaDeps) toolctx.ToolFunc {
	return func(_ context.Context, _ json.RawMessage) (string, error) {
		return "프로젝트 검색 기능이 비활성 상태입니다 (Vega 백엔드 제거됨).", nil
	}
}

// ToolProjectsGetField returns a tool that retrieves specific fields from a project.
// Vega backend removed — returns unavailable message.
func ToolProjectsGetField(_ *toolctx.VegaDeps) toolctx.ToolFunc {
	return func(_ context.Context, _ json.RawMessage) (string, error) {
		return "프로젝트 필드 조회 기능이 비활성 상태입니다 (Vega 백엔드 제거됨).", nil
	}
}

// ToolProjectsSearch returns a tool that performs natural-language search across projects.
// Vega backend removed — returns unavailable message.
func ToolProjectsSearch(_ *toolctx.VegaDeps) toolctx.ToolFunc {
	return func(_ context.Context, _ json.RawMessage) (string, error) {
		return "프로젝트 검색 기능이 비활성 상태입니다 (Vega 백엔드 제거됨).", nil
	}
}

// ToolProjectsGetDocument returns a tool that retrieves project documents.
// Vega backend removed — returns unavailable message.
func ToolProjectsGetDocument(_ *toolctx.VegaDeps) toolctx.ToolFunc {
	return func(_ context.Context, _ json.RawMessage) (string, error) {
		return "프로젝트 문서 조회 기능이 비활성 상태입니다 (Vega 백엔드 제거됨).", nil
	}
}

// ToolMemoryRecall returns an RLM-specific memory search tool.
// Memory/Vega backend removed — returns unavailable message.
func ToolMemoryRecall(_ *toolctx.VegaDeps) toolctx.ToolFunc {
	return func(_ context.Context, _ json.RawMessage) (string, error) {
		return "메모리 검색 기능이 비활성 상태입니다 (위키로 대체됨).", nil
	}
}
