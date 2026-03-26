// Package node manages node pairing, discovery, invocation, canvas capabilities,
// and the pending action queue.
//
// This ports the TypeScript node system (src/gateway/server-methods/nodes/nodes.ts)
// to Go, providing in-memory storage for paired/pending nodes.
package node

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// PairState represents the state of a pairing request.
type PairState string

const (
	PairStatePending  PairState = "pending"
	PairStateApproved PairState = "approved"
	PairStateRejected PairState = "rejected"
)

// PairRequest represents a pending pairing request from a node.
type PairRequest struct {
	RequestID       string    `json:"requestId"`
	NodeID          string    `json:"nodeId"`
	DisplayName     string    `json:"displayName,omitempty"`
	Platform        string    `json:"platform,omitempty"`
	Version         string    `json:"version,omitempty"`
	CoreVersion     string    `json:"coreVersion,omitempty"`
	UIVersion       string    `json:"uiVersion,omitempty"`
	DeviceFamily    string    `json:"deviceFamily,omitempty"`
	ModelIdentifier string    `json:"modelIdentifier,omitempty"`
	Caps            []string  `json:"caps,omitempty"`
	Commands        []string  `json:"commands,omitempty"`
	RemoteIP        string    `json:"remoteIp,omitempty"`
	Silent          bool      `json:"silent,omitempty"`
	State           PairState `json:"state"`
	CreatedAtMs     int64     `json:"createdAtMs"`
}

// PairedNode represents a successfully paired node.
type PairedNode struct {
	NodeID          string   `json:"nodeId"`
	DisplayName     string   `json:"displayName,omitempty"`
	Platform        string   `json:"platform,omitempty"`
	Version         string   `json:"version,omitempty"`
	CoreVersion     string   `json:"coreVersion,omitempty"`
	UIVersion       string   `json:"uiVersion,omitempty"`
	DeviceFamily    string   `json:"deviceFamily,omitempty"`
	ModelIdentifier string   `json:"modelIdentifier,omitempty"`
	Caps            []string `json:"caps,omitempty"`
	Commands        []string `json:"commands,omitempty"`
	Token           string   `json:"token"`
	PairedAtMs      int64    `json:"pairedAtMs"`
	Connected       bool     `json:"connected"`
	LastSeenAtMs    int64    `json:"lastSeenAtMs,omitempty"`
}

// NodeInfo is the external view of a node (paired or pending).
type NodeInfo struct {
	NodeID          string   `json:"nodeId"`
	DisplayName     string   `json:"displayName,omitempty"`
	Platform        string   `json:"platform,omitempty"`
	Version         string   `json:"version,omitempty"`
	CoreVersion     string   `json:"coreVersion,omitempty"`
	UIVersion       string   `json:"uiVersion,omitempty"`
	DeviceFamily    string   `json:"deviceFamily,omitempty"`
	ModelIdentifier string   `json:"modelIdentifier,omitempty"`
	Caps            []string `json:"caps,omitempty"`
	Commands        []string `json:"commands,omitempty"`
	Paired          bool     `json:"paired"`
	Connected       bool     `json:"connected"`
	LastSeenAtMs    int64    `json:"lastSeenAtMs,omitempty"`
}

// PendingAction represents an action queued for a node to pull.
type PendingAction struct {
	ID           string `json:"id"`
	Command      string `json:"command"`
	ParamsJSON   string `json:"paramsJSON,omitempty"`
	EnqueuedAtMs int64  `json:"enqueuedAtMs"`
	Priority     string `json:"priority,omitempty"`
	ExpiresAtMs  int64  `json:"expiresAtMs,omitempty"`
	Type         string `json:"type,omitempty"`
}

// CanvasCapability holds the current canvas capability token.
type CanvasCapability struct {
	Token       string `json:"canvasCapability"`
	ExpiresAtMs int64  `json:"canvasCapabilityExpiresAtMs"`
	HostURL     string `json:"canvasHostUrl"`
}

// InvokeRequest holds parameters for a node.invoke call.
type InvokeRequest struct {
	NodeID         string `json:"nodeId"`
	Command        string `json:"command"`
	Params         any    `json:"params,omitempty"`
	TimeoutMs      int64  `json:"timeoutMs,omitempty"`
	IdempotencyKey string `json:"idempotencyKey"`
}

// InvokeResult holds the result of a node.invoke call.
type InvokeResult struct {
	OK          bool   `json:"ok"`
	NodeID      string `json:"nodeId"`
	Command     string `json:"command"`
	Payload     any    `json:"payload,omitempty"`
	PayloadJSON string `json:"payloadJSON,omitempty"`
}

// Manager manages node pairing, discovery, and pending actions.
type Manager struct {
	mu             sync.RWMutex
	pairRequests   map[string]*PairRequest     // requestID -> PairRequest
	pairedNodes    map[string]*PairedNode      // nodeID -> PairedNode
	pendingActions map[string][]*PendingAction // nodeID -> actions
	canvas         *CanvasCapability

	// Invoke waiters: idempotencyKey -> result channel.
	invokeMu      sync.Mutex
	invokeWaiters map[string]chan *InvokeResult

	// Connected node tracking.
	connectedNodes map[string]bool
}

// NewManager creates a new node manager.
func NewManager() *Manager {
	return &Manager{
		pairRequests:   make(map[string]*PairRequest),
		pairedNodes:    make(map[string]*PairedNode),
		pendingActions: make(map[string][]*PendingAction),
		invokeWaiters:  make(map[string]chan *InvokeResult),
		connectedNodes: make(map[string]bool),
	}
}

// RequestPairing creates a new pairing request.
func (m *Manager) RequestPairing(params PairRequest) *PairRequest {
	reqID := generateNodeID()
	now := time.Now().UnixMilli()
	params.RequestID = reqID
	params.State = PairStatePending
	params.CreatedAtMs = now

	m.mu.Lock()
	m.pairRequests[reqID] = &params
	m.mu.Unlock()

	return &params
}

// ApprovePairing approves a pending pairing request.
func (m *Manager) ApprovePairing(requestID string) (*PairedNode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	req, ok := m.pairRequests[requestID]
	if !ok {
		return nil, fmt.Errorf("pair request %q not found", requestID)
	}
	if req.State != PairStatePending {
		return nil, fmt.Errorf("pair request %q already %s", requestID, req.State)
	}

	req.State = PairStateApproved
	token := generateToken()
	now := time.Now().UnixMilli()

	node := &PairedNode{
		NodeID:          req.NodeID,
		DisplayName:     req.DisplayName,
		Platform:        req.Platform,
		Version:         req.Version,
		CoreVersion:     req.CoreVersion,
		UIVersion:       req.UIVersion,
		DeviceFamily:    req.DeviceFamily,
		ModelIdentifier: req.ModelIdentifier,
		Caps:            req.Caps,
		Commands:        req.Commands,
		Token:           token,
		PairedAtMs:      now,
	}
	m.pairedNodes[req.NodeID] = node

	return node, nil
}

// RejectPairing rejects a pending pairing request.
func (m *Manager) RejectPairing(requestID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	req, ok := m.pairRequests[requestID]
	if !ok {
		return "", fmt.Errorf("pair request %q not found", requestID)
	}
	if req.State != PairStatePending {
		return "", fmt.Errorf("pair request %q already %s", requestID, req.State)
	}

	req.State = PairStateRejected
	return req.NodeID, nil
}

// VerifyToken checks if a node token is valid.
func (m *Manager) VerifyToken(nodeID, token string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	node, ok := m.pairedNodes[nodeID]
	return ok && node.Token == token
}

// ListPairRequests returns pending and paired node lists.
func (m *Manager) ListPairRequests() (pending []*PairRequest, paired []*PairedNode) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, req := range m.pairRequests {
		if req.State == PairStatePending {
			cp := *req
			pending = append(pending, &cp)
		}
	}
	for _, node := range m.pairedNodes {
		cp := *node
		cp.Connected = m.connectedNodes[node.NodeID]
		paired = append(paired, &cp)
	}
	return
}

// ListNodes returns info for all known nodes.
func (m *Manager) ListNodes() []NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var nodes []NodeInfo
	for _, node := range m.pairedNodes {
		nodes = append(nodes, NodeInfo{
			NodeID:          node.NodeID,
			DisplayName:     node.DisplayName,
			Platform:        node.Platform,
			Version:         node.Version,
			CoreVersion:     node.CoreVersion,
			UIVersion:       node.UIVersion,
			DeviceFamily:    node.DeviceFamily,
			ModelIdentifier: node.ModelIdentifier,
			Caps:            node.Caps,
			Commands:        node.Commands,
			Paired:          true,
			Connected:       m.connectedNodes[node.NodeID],
			LastSeenAtMs:    node.LastSeenAtMs,
		})
	}
	return nodes
}

// DescribeNode returns detailed info for a specific node.
func (m *Manager) DescribeNode(nodeID string) *NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	node, ok := m.pairedNodes[nodeID]
	if !ok {
		return nil
	}
	return &NodeInfo{
		NodeID:          node.NodeID,
		DisplayName:     node.DisplayName,
		Platform:        node.Platform,
		Version:         node.Version,
		CoreVersion:     node.CoreVersion,
		UIVersion:       node.UIVersion,
		DeviceFamily:    node.DeviceFamily,
		ModelIdentifier: node.ModelIdentifier,
		Caps:            node.Caps,
		Commands:        node.Commands,
		Paired:          true,
		Connected:       m.connectedNodes[node.NodeID],
		LastSeenAtMs:    node.LastSeenAtMs,
	}
}

// RenameNode renames a paired node.
func (m *Manager) RenameNode(nodeID, displayName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	node, ok := m.pairedNodes[nodeID]
	if !ok {
		return fmt.Errorf("node %q not found", nodeID)
	}
	node.DisplayName = displayName
	return nil
}

// SetConnected marks a node as connected or disconnected.
func (m *Manager) SetConnected(nodeID string, connected bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectedNodes[nodeID] = connected
	if node, ok := m.pairedNodes[nodeID]; ok && connected {
		node.LastSeenAtMs = time.Now().UnixMilli()
	}
}

// RefreshCanvasCapability generates a new canvas capability token.
func (m *Manager) RefreshCanvasCapability(hostURL string) *CanvasCapability {
	m.mu.Lock()
	defer m.mu.Unlock()

	token := generateToken()
	m.canvas = &CanvasCapability{
		Token:       token,
		ExpiresAtMs: time.Now().Add(24 * time.Hour).UnixMilli(),
		HostURL:     hostURL,
	}
	cp := *m.canvas
	return &cp
}

// GetCanvasCapability returns the current canvas capability.
func (m *Manager) GetCanvasCapability() *CanvasCapability {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.canvas == nil {
		return nil
	}
	cp := *m.canvas
	return &cp
}

// EnqueueAction adds a pending action for a node.
func (m *Manager) EnqueueAction(nodeID string, action PendingAction) *PendingAction {
	action.ID = generateNodeID()
	action.EnqueuedAtMs = time.Now().UnixMilli()

	m.mu.Lock()
	m.pendingActions[nodeID] = append(m.pendingActions[nodeID], &action)
	revision := len(m.pendingActions[nodeID])
	m.mu.Unlock()

	_ = revision
	return &action
}

// PullActions returns pending actions for the calling node (identified by nodeID).
func (m *Manager) PullActions(nodeID string) []*PendingAction {
	m.mu.RLock()
	defer m.mu.RUnlock()
	actions := m.pendingActions[nodeID]
	result := make([]*PendingAction, len(actions))
	for i, a := range actions {
		cp := *a
		result[i] = &cp
	}
	return result
}

// AckActions acknowledges (removes) actions by ID for a node.
func (m *Manager) AckActions(nodeID string, ids []string) (ackedIDs []string, remaining int) {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	actions := m.pendingActions[nodeID]
	var kept []*PendingAction
	for _, a := range actions {
		if idSet[a.ID] {
			ackedIDs = append(ackedIDs, a.ID)
		} else {
			kept = append(kept, a)
		}
	}
	m.pendingActions[nodeID] = kept
	return ackedIDs, len(kept)
}

// DrainActions drains pending actions with optional max limit.
func (m *Manager) DrainActions(nodeID string, maxItems int) (items []*PendingAction, hasMore bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	actions := m.pendingActions[nodeID]
	if maxItems <= 0 || maxItems >= len(actions) {
		items = actions
		m.pendingActions[nodeID] = nil
		return items, false
	}

	items = actions[:maxItems]
	m.pendingActions[nodeID] = actions[maxItems:]
	return items, true
}

// RegisterInvokeWaiter registers a channel to receive an invoke result.
func (m *Manager) RegisterInvokeWaiter(idempotencyKey string) chan *InvokeResult {
	ch := make(chan *InvokeResult, 1)
	m.invokeMu.Lock()
	m.invokeWaiters[idempotencyKey] = ch
	m.invokeMu.Unlock()
	return ch
}

// ResolveInvoke delivers an invoke result to the waiting caller.
func (m *Manager) ResolveInvoke(idempotencyKey string, result *InvokeResult) bool {
	m.invokeMu.Lock()
	ch, ok := m.invokeWaiters[idempotencyKey]
	if ok {
		delete(m.invokeWaiters, idempotencyKey)
	}
	m.invokeMu.Unlock()

	if ok {
		ch <- result
		return true
	}
	return false
}

func generateNodeID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func generateToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
