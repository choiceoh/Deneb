// Package server implements the HTTP + WebSocket gateway server.
//
// This will eventually replace the TypeScript implementation in
// src/gateway/server.impl.ts, providing concurrent connection handling
// via goroutines and integration with the Rust core via CGo.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// Server is the main gateway server.
type Server struct {
	addr       string
	httpServer *http.Server
	mu         sync.Mutex
	conns      map[string]*Connection
}

// Connection represents a connected client (WebSocket or HTTP).
type Connection struct {
	ID        string
	CreatedAt time.Time
}

// New creates a new gateway server bound to the given address.
func New(addr string) *Server {
	s := &Server{
		addr:  addr,
		conns: make(map[string]*Connection),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/rpc", s.handleRPC)
	mux.HandleFunc("/", s.handleRoot)

	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return s
}

// Run starts the server and blocks until the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.addr, err)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		log.Println("shutting down gateway server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// handleHealth responds with gateway health status.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"status":  "ok",
		"version": "0.1.0-go",
		"runtime": "go",
	}
	json.NewEncoder(w).Encode(resp)
}

// handleRPC is a placeholder for the JSON-RPC endpoint.
// This will be expanded to dispatch to internal/rpc handlers.
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params,omitempty"`
		ID     string          `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, "", "PARSE_ERROR", "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Method == "" || req.ID == "" {
		writeRPCError(w, req.ID, "INVALID_REQUEST", "method and id are required", http.StatusBadRequest)
		return
	}

	// Placeholder: echo the method back
	writeRPCResponse(w, req.ID, map[string]string{
		"echo":    req.Method,
		"status":  "not_implemented",
		"message": "Go gateway RPC is scaffolding only",
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"service": "deneb-gateway",
		"runtime": "go",
		"version": "0.1.0",
	})
}

func writeRPCResponse(w http.ResponseWriter, id string, result any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"type":    "res",
		"id":      id,
		"ok":      true,
		"payload": result,
	})
}

func writeRPCError(w http.ResponseWriter, id, code, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"type": "res",
		"id":   id,
		"ok":   false,
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
