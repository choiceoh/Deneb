package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
)

// Server is the MCP protocol handler. It routes incoming JSON-RPC requests
// to the appropriate handler and manages the MCP lifecycle.
type Server struct {
	transport *Transport
	bridge    *Bridge
	tools     *ToolRegistry
	resources *ResourceManager
	prompts   *PromptManager
	sampler   *Sampler
	events    *EventListener
	logger    *slog.Logger
	version   string

	clientCaps ClientCaps // set after initialize
}

// ServerConfig holds server configuration.
type ServerConfig struct {
	Bridge  *Bridge
	Logger  *slog.Logger
	Version string
}

// NewServer creates an MCP server.
func NewServer(transport *Transport, cfg ServerConfig) *Server {
	tools := NewToolRegistry()
	resources := NewResourceManager(cfg.Bridge)
	prompts := NewPromptManager(cfg.Bridge)
	sampler := NewSampler(transport, cfg.Bridge, cfg.Logger)
	events := NewEventListener(cfg.Bridge, transport, resources, sampler, cfg.Logger)

	return &Server{
		transport: transport,
		bridge:    cfg.Bridge,
		tools:     tools,
		resources: resources,
		prompts:   prompts,
		sampler:   sampler,
		events:    events,
		logger:    cfg.Logger,
		version:   cfg.Version,
	}
}

// Run processes MCP requests until stdin closes or context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	for {
		req, err := s.transport.ReadRequest()
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.logger.Info("stdin closed, shutting down")
				return nil
			}
			s.logger.Warn("read error", "err", err)
			continue
		}

		resp := s.handle(ctx, req)
		if resp == nil {
			// Notification — no response needed.
			continue
		}

		if err := s.transport.WriteResponse(resp); err != nil {
			s.logger.Error("write error", "err", err)
			return err
		}
	}
}

func (s *Server) handle(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(ctx, req)
	case "initialized":
		return nil // notification, no response
	case "ping":
		return s.handlePing(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "resources/read":
		return s.handleResourcesRead(ctx, req)
	case "resources/subscribe":
		return s.handleResourcesSubscribe(req)
	case "resources/unsubscribe":
		return s.handleResourcesUnsubscribe(req)
	case "prompts/list":
		return s.handlePromptsList(req)
	case "prompts/get":
		return s.handlePromptsGet(ctx, req)
	default:
		if req.IsNotification() {
			return nil
		}
		return MakeErrorResponse(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func (s *Server) handleInitialize(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	var params InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return MakeErrorResponse(req.ID, ErrCodeInvalidParams, "invalid initialize params")
	}

	s.clientCaps = params.Capabilities
	s.logger.Info("client connected",
		"client", params.ClientInfo.Name,
		"version", params.ClientInfo.Version,
		"protocol", params.ProtocolVersion,
	)

	// Enable sampling if client supports it.
	if params.Capabilities.Sampling != nil {
		s.sampler.SetEnabled(true)
		s.logger.Info("sampling enabled (client supports it)")
	}

	// Start event listener in background after initialization.
	go func() {
		if err := s.events.Run(ctx); err != nil && ctx.Err() == nil {
			s.logger.Warn("event listener stopped", "err", err)
		}
	}()

	result := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCaps{
			Tools:     &ToolsCap{},
			Resources: &ResourcesCap{Subscribe: true},
			Prompts:   &PromptsCap{},
			Experimental: map[string]any{
				"claude/channel": map[string]any{},
			},
		},
		ServerInfo: ServerInfo{
			Name:    "deneb",
			Version: s.version,
		},
	}

	// Only declare sampling if client supports it.
	if params.Capabilities.Sampling != nil {
		result.Capabilities.Sampling = &SamplingCap{}
	}

	resp, err := MakeResponse(req.ID, result)
	if err != nil {
		return MakeErrorResponse(req.ID, ErrCodeInternal, err.Error())
	}
	return resp
}

func (s *Server) handlePing(req *JSONRPCRequest) *JSONRPCResponse {
	resp, _ := MakeResponse(req.ID, map[string]any{})
	return resp
}

func (s *Server) handleToolsList(req *JSONRPCRequest) *JSONRPCResponse {
	result := ToolsListResult{Tools: s.tools.List()}
	resp, err := MakeResponse(req.ID, result)
	if err != nil {
		return MakeErrorResponse(req.ID, ErrCodeInternal, err.Error())
	}
	return resp
}

func (s *Server) handleToolsCall(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return MakeErrorResponse(req.ID, ErrCodeInvalidParams, "invalid tool call params")
	}

	rpcMethod, ok := s.tools.Lookup(params.Name)
	if !ok {
		return s.makeToolError(req.ID, fmt.Sprintf("unknown tool: %s", params.Name))
	}

	s.logger.Info("tool call", "tool", params.Name, "rpc", rpcMethod)

	payload, err := s.bridge.Call(ctx, rpcMethod, params.Arguments)
	if err != nil {
		return s.makeToolError(req.ID, err.Error())
	}

	result := ToolCallResult{
		Content: []ContentBlock{TextContent(prettyJSON(payload))},
	}

	resp, err := MakeResponse(req.ID, result)
	if err != nil {
		return MakeErrorResponse(req.ID, ErrCodeInternal, err.Error())
	}
	return resp
}

func (s *Server) handleResourcesList(req *JSONRPCRequest) *JSONRPCResponse {
	result := ResourcesListResult{Resources: s.resources.List()}
	resp, err := MakeResponse(req.ID, result)
	if err != nil {
		return MakeErrorResponse(req.ID, ErrCodeInternal, err.Error())
	}
	return resp
}

func (s *Server) handleResourcesRead(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	var params ResourceReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return MakeErrorResponse(req.ID, ErrCodeInvalidParams, "invalid resource read params")
	}

	result, err := s.resources.Read(ctx, params.URI)
	if err != nil {
		return MakeErrorResponse(req.ID, ErrCodeInternal, err.Error())
	}

	resp, mkErr := MakeResponse(req.ID, result)
	if mkErr != nil {
		return MakeErrorResponse(req.ID, ErrCodeInternal, mkErr.Error())
	}
	return resp
}

func (s *Server) handleResourcesSubscribe(req *JSONRPCRequest) *JSONRPCResponse {
	var params ResourceSubscribeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return MakeErrorResponse(req.ID, ErrCodeInvalidParams, "invalid subscribe params")
	}
	s.resources.Subscribe(params.URI)
	s.logger.Info("resource subscribed", "uri", params.URI)
	resp, _ := MakeResponse(req.ID, map[string]any{})
	return resp
}

func (s *Server) handleResourcesUnsubscribe(req *JSONRPCRequest) *JSONRPCResponse {
	var params ResourceSubscribeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return MakeErrorResponse(req.ID, ErrCodeInvalidParams, "invalid unsubscribe params")
	}
	s.resources.Unsubscribe(params.URI)
	resp, _ := MakeResponse(req.ID, map[string]any{})
	return resp
}

func (s *Server) handlePromptsList(req *JSONRPCRequest) *JSONRPCResponse {
	result := PromptsListResult{Prompts: s.prompts.List()}
	resp, err := MakeResponse(req.ID, result)
	if err != nil {
		return MakeErrorResponse(req.ID, ErrCodeInternal, err.Error())
	}
	return resp
}

func (s *Server) handlePromptsGet(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	var params PromptGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return MakeErrorResponse(req.ID, ErrCodeInvalidParams, "invalid prompt get params")
	}

	result, err := s.prompts.Get(ctx, params.Name, params.Arguments)
	if err != nil {
		return MakeErrorResponse(req.ID, ErrCodeInternal, err.Error())
	}

	resp, mkErr := MakeResponse(req.ID, result)
	if mkErr != nil {
		return MakeErrorResponse(req.ID, ErrCodeInternal, mkErr.Error())
	}
	return resp
}

func (s *Server) makeToolError(id json.RawMessage, msg string) *JSONRPCResponse {
	result := ToolCallResult{
		Content: []ContentBlock{TextContent(msg)},
		IsError: true,
	}
	resp, _ := MakeResponse(id, result)
	return resp
}
