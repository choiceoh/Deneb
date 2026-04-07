package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
)

// Sampler sends sampling/createMessage requests to Claude Desktop
// when gateway events require AI analysis.
type Sampler struct {
	transport *Transport
	bridge    *Bridge
	logger    *slog.Logger
	enabled   atomic.Bool // set when client declares sampling capability
	nextID    atomic.Int64
}

// NewSampler creates a sampler.
func NewSampler(transport *Transport, bridge *Bridge, logger *slog.Logger) *Sampler {
	s := &Sampler{
		transport: transport,
		bridge:    bridge,
		logger:    logger,
	}
	s.nextID.Store(1000) // start IDs at 1000 to avoid collisions with client IDs
	return s
}

// SetEnabled enables or disables sampling based on client capabilities.
func (s *Sampler) SetEnabled(enabled bool) {
	s.enabled.Store(enabled)
}

// HandleEvent processes a gateway event and optionally sends a sampling
// request to Claude Desktop for analysis.
func (s *Sampler) HandleEvent(ctx context.Context, eventName string, payload json.RawMessage) {
	if !s.enabled.Load() {
		return
	}

	req, err := s.buildSamplingRequest(ctx, eventName, payload)
	if err != nil {
		s.logger.Warn("failed to build sampling request", "event", eventName, "err", err)
		return
	}

	id := s.nextID.Add(1)
	idBytes, _ := json.Marshal(id)

	rpcReq := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      idBytes,
		Method:  "sampling/createMessage",
	}
	paramsBytes, _ := json.Marshal(req)
	rpcReq.Params = paramsBytes

	if err := s.transport.SendRequest(rpcReq); err != nil {
		s.logger.Warn("failed to send sampling request", "event", eventName, "err", err)
		return
	}

	s.logger.Info("sampling request sent", "event", eventName, "id", id)
	// The response will be handled by the main transport loop in server.go.
}

func (s *Sampler) buildSamplingRequest(ctx context.Context, eventName string, payload json.RawMessage) (*SamplingRequest, error) {
	switch eventName {
	case "session.failed":
		return s.buildSessionFailedRequest(ctx, payload)
	case "agent.completed":
		return s.buildAgentCompletedRequest(ctx, payload)
	case "cron.fired":
		return s.buildCronFiredRequest(ctx, payload)
	default:
		return nil, fmt.Errorf("unhandled event: %s", eventName)
	}
}

func (s *Sampler) buildSessionFailedRequest(ctx context.Context, payload json.RawMessage) (*SamplingRequest, error) {
	context := fmt.Sprintf("Deneb 세션이 실패했습니다.\n\n이벤트 데이터:\n```json\n%s\n```", prettyJSON(payload))

	return &SamplingRequest{
		Messages: []SamplingMessage{
			{
				Role:    "user",
				Content: TextContent(context + "\n\n이 에러를 분석하고 원인과 해결 방법을 한국어로 설명해주세요."),
			},
		},
		SystemPrompt:   "당신은 Deneb AI 시스템 관리 도우미입니다. 시스템 이벤트를 분석하고 한국어로 명확하게 설명합니다.",
		MaxTokens:      1024,
		IncludeContext: "thisServer",
		ModelPreferences: &ModelPrefs{
			IntelligencePriority: 0.8,
			SpeedPriority:        0.5,
		},
	}, nil
}

func (s *Sampler) buildAgentCompletedRequest(ctx context.Context, payload json.RawMessage) (*SamplingRequest, error) {
	context := fmt.Sprintf("Deneb 에이전트 작업이 완료되었습니다.\n\n이벤트 데이터:\n```json\n%s\n```", prettyJSON(payload))

	return &SamplingRequest{
		Messages: []SamplingMessage{
			{
				Role:    "user",
				Content: TextContent(context + "\n\n이 작업 결과를 간략히 요약해주세요."),
			},
		},
		SystemPrompt:   "당신은 Deneb AI 시스템 관리 도우미입니다. 에이전트 작업 결과를 간결하게 요약합니다.",
		MaxTokens:      512,
		IncludeContext: "thisServer",
		ModelPreferences: &ModelPrefs{
			SpeedPriority:        0.8,
			IntelligencePriority: 0.5,
		},
	}, nil
}

func (s *Sampler) buildCronFiredRequest(ctx context.Context, payload json.RawMessage) (*SamplingRequest, error) {
	context := fmt.Sprintf("Deneb 크론 작업이 실행되었습니다.\n\n이벤트 데이터:\n```json\n%s\n```", prettyJSON(payload))

	return &SamplingRequest{
		Messages: []SamplingMessage{
			{
				Role:    "user",
				Content: TextContent(context + "\n\n실행 결과에 이상 징후가 있는지 확인해주세요."),
			},
		},
		SystemPrompt:   "당신은 Deneb AI 시스템 모니터링 도우미입니다. 크론 작업 결과에서 이상 징후를 감지합니다.",
		MaxTokens:      512,
		IncludeContext: "thisServer",
		ModelPreferences: &ModelPrefs{
			SpeedPriority: 0.9,
		},
	}, nil
}
