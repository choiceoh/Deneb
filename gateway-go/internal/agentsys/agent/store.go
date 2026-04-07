// Package agent manages agent CRUD operations and file storage.
//
// This ports the TypeScript agent system (src/gateway/server-methods/agents/agents.ts)
// to Go, providing in-memory agent registry and file operations.
package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Agent represents a registered agent entry.
type Agent struct {
	AgentID      string            `json:"agentId"`
	Name         string            `json:"name,omitempty"`
	Description  string            `json:"description,omitempty"`
	Model        string            `json:"model,omitempty"`
	SystemPrompt string            `json:"systemPrompt,omitempty"`
	Tools        []string          `json:"tools,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	CreatedAtMs  int64             `json:"createdAtMs"`
	UpdatedAtMs  int64             `json:"updatedAtMs"`
}

// FileEntry represents a file belonging to an agent.
type FileEntry struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	Missing       bool   `json:"missing"`
	Size          int64  `json:"size,omitempty"`
	UpdatedAtMs   int64  `json:"updatedAtMs,omitempty"`
	ContentBase64 string `json:"contentBase64,omitempty"`
}

// Store manages agents and their files in memory.
type Store struct {
	mu     sync.RWMutex
	agents map[string]*Agent
	files  map[string]map[string]*FileEntry // agentID -> fileName -> FileEntry
}

// NewStore creates a new agent store.
func NewStore() *Store {
	return &Store{
		agents: make(map[string]*Agent),
		files:  make(map[string]map[string]*FileEntry),
	}
}

// List returns all agents.
func (s *Store) List() []*Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Agent, 0, len(s.agents))
	for _, a := range s.agents {
		cp := *a
		result = append(result, &cp)
	}
	return result
}

// Get returns an agent by ID, or nil if not found.
func (s *Store) Get(agentID string) *Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.agents[agentID]
	if a == nil {
		return nil
	}
	cp := *a
	return &cp
}

// Create creates a new agent and returns it.
func (s *Store) Create(params CreateParams) *Agent {
	id := params.AgentID
	if id == "" {
		id = genAgentID()
	}
	now := time.Now().UnixMilli()

	agent := &Agent{
		AgentID:      id,
		Name:         params.Name,
		Description:  params.Description,
		Model:        params.Model,
		SystemPrompt: params.SystemPrompt,
		Tools:        params.Tools,
		Metadata:     params.Metadata,
		CreatedAtMs:  now,
		UpdatedAtMs:  now,
	}

	s.mu.Lock()
	s.agents[id] = agent
	s.files[id] = make(map[string]*FileEntry)
	s.mu.Unlock()

	cp := *agent
	return &cp
}

// CreateParams holds parameters for creating an agent.
type CreateParams struct {
	AgentID      string
	Name         string
	Description  string
	Model        string
	SystemPrompt string
	Tools        []string
	Metadata     map[string]string
}

// Update applies a patch to an existing agent.
func (s *Store) Update(agentID string, patch map[string]any) (*Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.agents[agentID]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", agentID)
	}

	if v, ok := patch["name"]; ok {
		if str, ok := v.(string); ok {
			a.Name = str
		}
	}
	if v, ok := patch["description"]; ok {
		if str, ok := v.(string); ok {
			a.Description = str
		}
	}
	if v, ok := patch["model"]; ok {
		if str, ok := v.(string); ok {
			a.Model = str
		}
	}
	if v, ok := patch["systemPrompt"]; ok {
		if str, ok := v.(string); ok {
			a.SystemPrompt = str
		}
	}
	if v, ok := patch["tools"]; ok {
		if arr, ok := v.([]any); ok {
			tools := make([]string, 0, len(arr))
			for _, item := range arr {
				if str, ok := item.(string); ok {
					tools = append(tools, str)
				}
			}
			a.Tools = tools
		}
	}
	a.UpdatedAtMs = time.Now().UnixMilli()

	cp := *a
	return &cp, nil
}

// Delete removes an agent by ID.
func (s *Store) Delete(agentID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.agents[agentID]; !ok {
		return false
	}
	delete(s.agents, agentID)
	delete(s.files, agentID)
	return true
}

// ListFiles returns all file entries for an agent.
func (s *Store) ListFiles(agentID string) ([]*FileEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.agents[agentID]; !ok {
		return nil, fmt.Errorf("agent %q not found", agentID)
	}
	filesMap := s.files[agentID]
	result := make([]*FileEntry, 0, len(filesMap))
	for _, f := range filesMap {
		cp := *f
		cp.ContentBase64 = "" // Don't include content in list.
		result = append(result, &cp)
	}
	return result, nil
}

// GetFile returns a specific file for an agent.
func (s *Store) GetFile(agentID, name string) (*FileEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.agents[agentID]; !ok {
		return nil, fmt.Errorf("agent %q not found", agentID)
	}
	filesMap := s.files[agentID]
	f, ok := filesMap[name]
	if !ok {
		return nil, fmt.Errorf("file %q not found for agent %q", name, agentID)
	}
	cp := *f
	return &cp, nil
}

// SetFile creates or updates a file for an agent.
func (s *Store) SetFile(agentID, name, contentBase64 string) (*FileEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.agents[agentID]; !ok {
		return nil, fmt.Errorf("agent %q not found", agentID)
	}

	now := time.Now().UnixMilli()
	entry := &FileEntry{
		Name:          name,
		Path:          fmt.Sprintf("agents/%s/%s", agentID, name),
		Missing:       false,
		Size:          int64(len(contentBase64) * 3 / 4), // approximate decoded size
		UpdatedAtMs:   now,
		ContentBase64: contentBase64,
	}

	if s.files[agentID] == nil {
		s.files[agentID] = make(map[string]*FileEntry)
	}
	s.files[agentID][name] = entry

	cp := *entry
	return &cp, nil
}

func genAgentID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("agent-%d", time.Now().UnixNano())
	}
	return "agent-" + hex.EncodeToString(b)
}
