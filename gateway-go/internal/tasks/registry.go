package tasks

import (
	"fmt"
	"log/slog"
	"sync"
)

// Registry is the in-memory task registry backed by a SQLite store.
// It maintains secondary indexes for fast lookups by runID, ownerKey,
// childSessionKey, and flowID.
type Registry struct {
	mu    sync.RWMutex
	store *Store

	// Primary index.
	tasks map[string]*TaskRecord

	// Secondary indexes.
	byRunID           map[string]string // runID -> taskID
	byOwnerKey        map[string][]string
	byChildSessionKey map[string]string // childSessionKey -> taskID
	byFlowID          map[string][]string

	// Flow index.
	flows map[string]*FlowRecord

	logger *slog.Logger
}

// NewRegistry creates a registry, loading all existing state from the store.
func NewRegistry(store *Store, logger *slog.Logger) (*Registry, error) {
	if logger == nil {
		logger = slog.Default()
	}
	r := &Registry{
		store:             store,
		tasks:             make(map[string]*TaskRecord),
		byRunID:           make(map[string]string),
		byOwnerKey:        make(map[string][]string),
		byChildSessionKey: make(map[string]string),
		byFlowID:          make(map[string][]string),
		flows:             make(map[string]*FlowRecord),
		logger:            logger,
	}

	if err := r.restore(); err != nil {
		return nil, fmt.Errorf("task registry restore: %w", err)
	}
	return r, nil
}

// restore loads all tasks and flows from the store into memory.
func (r *Registry) restore() error {
	tasks, err := r.store.ListAll()
	if err != nil {
		return err
	}
	for _, t := range tasks {
		r.tasks[t.TaskID] = t
		r.indexTask(t)
	}

	flows, err := r.store.ListFlows()
	if err != nil {
		return err
	}
	for _, f := range flows {
		r.flows[f.FlowID] = f
	}

	r.logger.Info("task registry restored",
		"tasks", len(r.tasks), "flows", len(r.flows))
	return nil
}

// --- Task Operations ---

// Put inserts or updates a task in both memory and store.
func (r *Registry) Put(t *TaskRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Remove old index entries if updating.
	if old, ok := r.tasks[t.TaskID]; ok {
		r.deindexTask(old)
	}

	r.tasks[t.TaskID] = t
	r.indexTask(t)

	if err := r.store.UpsertTask(t); err != nil {
		r.logger.Error("task registry: store upsert failed", "taskId", t.TaskID, "error", err)
		return err
	}
	return nil
}

// Get returns a task by ID, or nil if not found.
func (r *Registry) Get(taskID string) *TaskRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t := r.tasks[taskID]
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

// GetByRunID returns the task associated with a run ID.
func (r *Registry) GetByRunID(runID string) *TaskRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	taskID, ok := r.byRunID[runID]
	if !ok {
		return nil
	}
	t := r.tasks[taskID]
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

// GetByChildSessionKey returns the task associated with a child session key.
func (r *Registry) GetByChildSessionKey(key string) *TaskRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	taskID, ok := r.byChildSessionKey[key]
	if !ok {
		return nil
	}
	t := r.tasks[taskID]
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

// ListAll returns all tasks.
func (r *Registry) ListAll() []*TaskRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*TaskRecord, 0, len(r.tasks))
	for _, t := range r.tasks {
		cp := *t
		out = append(out, &cp)
	}
	return out
}

// ListActive returns all active (non-terminal) tasks.
func (r *Registry) ListActive() []*TaskRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*TaskRecord
	for _, t := range r.tasks {
		if t.Status.IsActive() {
			cp := *t
			out = append(out, &cp)
		}
	}
	return out
}

// ListByOwner returns tasks for a specific owner.
func (r *Registry) ListByOwner(ownerKey string) []*TaskRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.byOwnerKey[ownerKey]
	out := make([]*TaskRecord, 0, len(ids))
	for _, id := range ids {
		if t := r.tasks[id]; t != nil {
			cp := *t
			out = append(out, &cp)
		}
	}
	return out
}

// ListByRuntime returns tasks for a specific runtime.
func (r *Registry) ListByRuntime(runtime TaskRuntime) []*TaskRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*TaskRecord
	for _, t := range r.tasks {
		if t.Runtime == runtime {
			cp := *t
			out = append(out, &cp)
		}
	}
	return out
}

// ListByFlowID returns all tasks belonging to a flow.
func (r *Registry) ListByFlowID(flowID string) []*TaskRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.byFlowID[flowID]
	out := make([]*TaskRecord, 0, len(ids))
	for _, id := range ids {
		if t := r.tasks[id]; t != nil {
			cp := *t
			out = append(out, &cp)
		}
	}
	return out
}

// Delete removes a task from both memory and store.
func (r *Registry) Delete(taskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if t, ok := r.tasks[taskID]; ok {
		r.deindexTask(t)
		delete(r.tasks, taskID)
	}

	return r.store.DeleteTask(taskID)
}

// Summary returns aggregate statistics.
func (r *Registry) Summary() *RegistrySummary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sum := &RegistrySummary{
		ByStatus:  make(map[TaskStatus]int),
		ByRuntime: make(map[TaskRuntime]int),
	}
	for _, t := range r.tasks {
		sum.Total++
		sum.ByStatus[t.Status]++
		sum.ByRuntime[t.Runtime]++
		if t.Status.IsActive() {
			sum.Active++
		}
		if t.Status.IsTerminal() {
			sum.Terminal++
		}
		if t.Status == StatusFailed || t.Status == StatusTimedOut {
			sum.Failures++
		}
	}
	return sum
}

// --- Flow Operations ---

// PutFlow inserts or updates a flow.
func (r *Registry) PutFlow(f *FlowRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.flows[f.FlowID] = f
	return r.store.UpsertFlow(f)
}

// GetFlow returns a flow by ID.
func (r *Registry) GetFlow(flowID string) *FlowRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f := r.flows[flowID]
	if f == nil {
		return nil
	}
	cp := *f
	return &cp
}

// ListFlows returns all flows.
func (r *Registry) ListFlows() []*FlowRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*FlowRecord, 0, len(r.flows))
	for _, f := range r.flows {
		cp := *f
		out = append(out, &cp)
	}
	return out
}

// ListActiveFlows returns non-terminal flows.
func (r *Registry) ListActiveFlows() []*FlowRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*FlowRecord
	for _, f := range r.flows {
		if f.Status == FlowActive || f.Status == FlowBlocked {
			cp := *f
			out = append(out, &cp)
		}
	}
	return out
}

// DeleteFlow removes a flow.
func (r *Registry) DeleteFlow(flowID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.flows, flowID)
	return r.store.DeleteFlow(flowID)
}

// RefreshFlowCounts recalculates task counts for a flow from its tasks.
func (r *Registry) RefreshFlowCounts(flowID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	f := r.flows[flowID]
	if f == nil {
		return fmt.Errorf("flow not found: %s", flowID)
	}

	ids := r.byFlowID[flowID]
	f.TaskCount = len(ids)
	f.CompletedCount = 0
	f.FailedCount = 0

	allDone := true
	anyFailed := false
	anyBlocked := false

	for _, id := range ids {
		t := r.tasks[id]
		if t == nil {
			continue
		}
		switch t.Status {
		case StatusSucceeded:
			f.CompletedCount++
		case StatusFailed, StatusTimedOut, StatusLost:
			f.FailedCount++
			anyFailed = true
		case StatusBlocked:
			anyBlocked = true
			allDone = false
		default:
			if t.Status.IsActive() {
				allDone = false
			}
		}
	}

	f.UpdatedAt = NowMs()

	// Auto-transition flow status based on task states.
	if f.TaskCount > 0 && allDone {
		if anyFailed {
			f.Status = FlowFailed
		} else {
			f.Status = FlowCompleted
		}
		f.CompletedAt = NowMs()
	} else if anyBlocked {
		f.Status = FlowBlocked
	}

	return r.store.UpsertFlow(f)
}

// --- Store Delegation ---

// ListEvents returns audit trail events for a task.
func (r *Registry) ListEvents(taskID string) ([]*TaskEventRecord, error) {
	return r.store.ListEvents(taskID)
}

// --- Index Management ---

func (r *Registry) indexTask(t *TaskRecord) {
	if t.RunID != "" {
		r.byRunID[t.RunID] = t.TaskID
	}
	if t.OwnerKey != "" {
		r.byOwnerKey[t.OwnerKey] = append(r.byOwnerKey[t.OwnerKey], t.TaskID)
	}
	if t.ChildSessionKey != "" {
		r.byChildSessionKey[t.ChildSessionKey] = t.TaskID
	}
	if t.FlowID != "" {
		r.byFlowID[t.FlowID] = append(r.byFlowID[t.FlowID], t.TaskID)
	}
}

func (r *Registry) deindexTask(t *TaskRecord) {
	if t.RunID != "" {
		delete(r.byRunID, t.RunID)
	}
	if t.OwnerKey != "" {
		r.byOwnerKey[t.OwnerKey] = removeFromSlice(r.byOwnerKey[t.OwnerKey], t.TaskID)
		if len(r.byOwnerKey[t.OwnerKey]) == 0 {
			delete(r.byOwnerKey, t.OwnerKey)
		}
	}
	if t.ChildSessionKey != "" {
		delete(r.byChildSessionKey, t.ChildSessionKey)
	}
	if t.FlowID != "" {
		r.byFlowID[t.FlowID] = removeFromSlice(r.byFlowID[t.FlowID], t.TaskID)
		if len(r.byFlowID[t.FlowID]) == 0 {
			delete(r.byFlowID, t.FlowID)
		}
	}
}

func removeFromSlice(s []string, v string) []string {
	for i, x := range s {
		if x == v {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}
