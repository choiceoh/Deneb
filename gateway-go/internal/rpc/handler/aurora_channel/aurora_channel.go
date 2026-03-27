// Package aurora_channel provides RPC method handlers for Aurora desktop app communication.
//
// Aurora is a desktop AI coding assistant that connects to Deneb via RPC
// to leverage Deneb's AI agent capabilities, memory, and tools.
package aurora_channel

import (
	"context"

	chatpkg "github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds dependencies for Aurora channel RPC methods.
type Deps struct {
	Chat *chatpkg.Handler
}

// Methods returns Aurora RPC handlers keyed by method name.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"aurora.ping":   handlePing(),
		"aurora.chat":   handleChat(deps),
		"aurora.memory": handleMemory(deps),
	}
}

// handlePing returns a simple health check handler for Aurora connectivity.
// No params required. Returns {"ok": true}.
func handlePing() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return protocol.MustResponseOK(req.ID, map[string]any{
			"ok": true,
		})
	}
}

// handleChat processes a chat message from Aurora via Deneb's chat pipeline.
// Uses SendSync for synchronous response (Aurora needs full text back).
//
// Params:
//   - message (string, required): The message to process.
//   - sessionKey (string, optional): Session identifier. Defaults to "aurora".
//   - model (string, optional): Model override.
//
// Returns:
//   - text (string): The response text from the AI agent.
//   - model (string): The model used for generation.
//   - tokens (object): Token usage {input, output, total}.
func handleChat(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Message    string `json:"message"`
			SessionKey string `json:"sessionKey"`
			Model      string `json:"model"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return rpcerr.New(protocol.ErrInvalidRequest, "params required").Response(req.ID)
		}
		if p.Message == "" {
			return rpcerr.MissingParam("message").Response(req.ID)
		}
		if p.SessionKey == "" {
			p.SessionKey = "aurora"
		}

		if deps.Chat == nil {
			return rpcerr.Unavailable("chat handler not available").Response(req.ID)
		}

		result, err := deps.Chat.SendSync(ctx, p.SessionKey, p.Message, p.Model)
		if err != nil {
			return rpcerr.New(protocol.ErrDependencyFailed, "aurora.chat failed: "+err.Error()).Response(req.ID)
		}

		return protocol.MustResponseOK(req.ID, map[string]any{
			"text":  result.Text,
			"model": result.Model,
			"tokens": map[string]int{
				"input":  result.InputTokens,
				"output": result.OutputTokens,
				"total":  result.InputTokens + result.OutputTokens,
			},
		})
	}
}

// handleMemory searches Deneb's memory for relevant information.
// Uses SendSync with a memory-search-oriented prompt.
//
// Params:
//   - query (string, required): The search query.
//   - sessionKey (string, optional): Session identifier. Defaults to "aurora-memory".
//
// Returns:
//   - text (string): The search results.
//   - model (string): The model used.
func handleMemory(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Query      string `json:"query"`
			SessionKey string `json:"sessionKey"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return rpcerr.New(protocol.ErrInvalidRequest, "params required").Response(req.ID)
		}
		if p.Query == "" {
			return rpcerr.MissingParam("query").Response(req.ID)
		}
		if p.SessionKey == "" {
			p.SessionKey = "aurora-memory"
		}

		if deps.Chat == nil {
			return rpcerr.Unavailable("chat handler not available").Response(req.ID)
		}

		// Frame the query as a memory search request
		prompt := "다음 내용을 메모리에서 검색하여 관련 정보를 찾아줘: " + p.Query
		result, err := deps.Chat.SendSync(ctx, p.SessionKey, prompt, "")
		if err != nil {
			return rpcerr.New(protocol.ErrDependencyFailed, "aurora.memory failed: "+err.Error()).Response(req.ID)
		}

		return protocol.MustResponseOK(req.ID, map[string]any{
			"text":  result.Text,
			"model": result.Model,
		})
	}
}
