package tasks

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// Store provides file-backed persistence for the task ledger.
// Tasks and flows are kept in memory and atomically snapshotted to a JSON file.
// Events are append-only in a separate JSONL file.
//
// Write coalescing: mutations mark the store dirty instead of writing immediately.
// A background goroutine flushes at most once per second, batching burst mutations
// into a single atomic write.
type Store struct {
	mu     sync.RWMutex
	dir    string
	logger *slog.Logger

	tasks map[string]*TaskRecord
	flows map[string]*FlowRecord

	// Write coalescing.
	dirty     bool
	flushCh   chan struct{} // signals the flush goroutine
	done      chan struct{} // closed on Close to stop the flush goroutine
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// StoreConfig configures the task store.
type StoreConfig struct {
	DatabasePath string `json:"databasePath"`
}

// DefaultStoreConfig returns production defaults.
func DefaultStoreConfig() StoreConfig {
	home, _ := os.UserHomeDir()
	return StoreConfig{
		DatabasePath: filepath.Join(home, ".deneb", "tasks.db"),
	}
}

// snapshotData is the on-disk format for the task/flow snapshot.
type snapshotData struct {
	Tasks []*TaskRecord `json:"tasks,omitempty"`
	Flows []*FlowRecord `json:"flows,omitempty"`
}

// OpenStore opens or creates the task ledger.
// The DatabasePath config is reinterpreted as a directory base for file storage.
func OpenStore(cfg StoreConfig, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}

	dir := filepath.Dir(cfg.DatabasePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("tasks store: mkdir %s: %w", dir, err)
	}

	s := &Store{
		dir:     dir,
		logger:  logger,
		tasks:   make(map[string]*TaskRecord),
		flows:   make(map[string]*FlowRecord),
		flushCh: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}

	// Load existing snapshot.
	snapPath := s.snapshotPath()
	data, err := os.ReadFile(snapPath)
	if err == nil {
		var snap snapshotData
		if err := json.Unmarshal(data, &snap); err != nil {
			logger.Warn("tasks store: corrupt snapshot, starting fresh", "error", err)
		} else {
			for _, t := range snap.Tasks {
				s.tasks[t.TaskID] = t
			}
			for _, f := range snap.Flows {
				s.flows[f.FlowID] = f
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("tasks store: read snapshot: %w", err)
	}

	// Start background flush goroutine.
	s.wg.Add(1)
	go s.flushLoop()

	return s, nil
}

func (s *Store) snapshotPath() string {
	return filepath.Join(s.dir, "tasks.json")
}

func (s *Store) eventsPath() string {
	return filepath.Join(s.dir, "task_events.jsonl")
}

// markDirty signals that in-memory state has changed and needs to be flushed.
// Must be called with mu held.
func (s *Store) markDirty() {
	s.dirty = true
	// Non-blocking signal to the flush goroutine.
	select {
	case s.flushCh <- struct{}{}:
	default:
	}
}

// flushLoop runs in the background and coalesces writes.
// After receiving a dirty signal, it waits up to 1 second to batch
// more mutations, then writes a single snapshot.
func (s *Store) flushLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			// Final flush on shutdown.
			s.flushIfDirty()
			return
		case <-s.flushCh:
			// Debounce: wait a short window to batch more mutations.
			select {
			case <-s.done:
				s.flushIfDirty()
				return
			case <-time.After(time.Second):
			}
			s.flushIfDirty()
		}
	}
}

func (s *Store) flushIfDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.dirty {
		return
	}
	if err := s.writeSnapshot(); err != nil {
		s.logger.Error("tasks store: flush failed", "error", err)
		return
	}
	s.dirty = false
}

// writeSnapshot atomically writes the current state. Must be called with mu held.
func (s *Store) writeSnapshot() error {
	snap := snapshotData{
		Tasks: make([]*TaskRecord, 0, len(s.tasks)),
		Flows: make([]*FlowRecord, 0, len(s.flows)),
	}
	for _, t := range s.tasks {
		snap.Tasks = append(snap.Tasks, t)
	}
	for _, f := range s.flows {
		snap.Flows = append(snap.Flows, f)
	}

	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("tasks store: marshal snapshot: %w", err)
	}
	return atomicfile.WriteFile(s.snapshotPath(), data, &atomicfile.Options{Fsync: true})
}

// Close flushes pending writes and stops the background goroutine.
// Safe to call multiple times.
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
	})
	s.wg.Wait()
	return nil
}

// --- Task CRUD ---

// UpsertTask inserts or updates a task record.
func (s *Store) UpsertTask(t *TaskRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tasks[t.TaskID] = t
	s.markDirty()
	return nil
}

// Task retrieves a single task by ID.
func (s *Store) Task(taskID string) (*TaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t := s.tasks[taskID]
	if t == nil {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}

// TaskByRunID retrieves a task by its run ID.
func (s *Store) TaskByRunID(runID string) (*TaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var best *TaskRecord
	for _, t := range s.tasks {
		if t.RunID == runID {
			if best == nil || t.CreatedAt > best.CreatedAt {
				best = t
			}
		}
	}
	if best == nil {
		return nil, nil
	}
	cp := *best
	return &cp, nil
}

// ListAll returns all tasks ordered by creation time.
func (s *Store) ListAll() ([]*TaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sortedTasks(func(*TaskRecord) bool { return true }), nil
}

// ListByStatus returns tasks matching the given status.
func (s *Store) ListByStatus(status TaskStatus) ([]*TaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sortedTasks(func(t *TaskRecord) bool { return t.Status == status }), nil
}

// ListActive returns all queued, running, or blocked tasks.
func (s *Store) ListActive() ([]*TaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sortedTasks(func(t *TaskRecord) bool { return t.Status.IsActive() }), nil
}

// ListByOwner returns tasks for a specific owner key.
func (s *Store) ListByOwner(ownerKey string) ([]*TaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sortedTasks(func(t *TaskRecord) bool { return t.OwnerKey == ownerKey }), nil
}

// ListByFlowID returns all tasks belonging to a flow.
func (s *Store) ListByFlowID(flowID string) ([]*TaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sortedTasks(func(t *TaskRecord) bool { return t.FlowID == flowID }), nil
}

// ListByRuntime returns tasks for a specific runtime.
func (s *Store) ListByRuntime(runtime TaskRuntime) ([]*TaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sortedTasks(func(t *TaskRecord) bool { return t.Runtime == runtime }), nil
}

func (s *Store) sortedTasks(filter func(*TaskRecord) bool) []*TaskRecord {
	var out []*TaskRecord
	for _, t := range s.tasks {
		if filter(t) {
			cp := *t
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

// DeleteTask removes a task and its events.
func (s *Store) DeleteTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.tasks, taskID)
	s.markDirty()
	return nil
}

// DeleteTerminalBefore removes terminal tasks older than the given timestamp.
// Also prunes orphaned events from the event log.
func (s *Store) DeleteTerminalBefore(beforeMs int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int64
	for id, t := range s.tasks {
		if t.Status.IsTerminal() && t.CleanupAfter != 0 && t.CleanupAfter < beforeMs {
			delete(s.tasks, id)
			count++
		}
	}
	if count > 0 {
		s.markDirty()
		// Prune events: rewrite event log keeping only events for surviving tasks.
		s.pruneEvents()
	}
	return count, nil
}

// pruneEvents rewrites the event log to keep only events for tasks that still exist.
// Must be called with mu held.
func (s *Store) pruneEvents() {
	evPath := s.eventsPath()
	all, err := jsonlstore.Load[TaskEventRecord](evPath)
	if err != nil || len(all) == 0 {
		return
	}

	var kept []TaskEventRecord
	for _, e := range all {
		if _, exists := s.tasks[e.TaskID]; exists {
			kept = append(kept, e)
		}
	}

	// Only rewrite if we actually pruned something.
	if len(kept) < len(all) {
		if err := jsonlstore.Snapshot(evPath, kept); err != nil {
			s.logger.Error("tasks store: prune events failed", "error", err)
		}
	}
}

// --- Task Events ---

// AppendEvent records an audit trail entry.
func (s *Store) AppendEvent(evt *TaskEventRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return jsonlstore.Append(s.eventsPath(), evt)
}

// ListEvents returns all events for a task, ordered chronologically.
func (s *Store) ListEvents(taskID string) ([]*TaskEventRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all, err := jsonlstore.Load[TaskEventRecord](s.eventsPath())
	if err != nil {
		return nil, err
	}

	var events []*TaskEventRecord
	for i := range all {
		if all[i].TaskID == taskID {
			e := all[i]
			events = append(events, &e)
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].At < events[j].At })
	return events, nil
}

// --- Flow CRUD ---

// UpsertFlow inserts or updates a flow record.
func (s *Store) UpsertFlow(f *FlowRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.flows[f.FlowID] = f
	s.markDirty()
	return nil
}

// Flow retrieves a flow by ID.
func (s *Store) Flow(flowID string) (*FlowRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	f := s.flows[flowID]
	if f == nil {
		return nil, nil
	}
	cp := *f
	return &cp, nil
}

// ListFlows returns all flows ordered by creation time (newest first).
func (s *Store) ListFlows() ([]*FlowRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*FlowRecord, 0, len(s.flows))
	for _, f := range s.flows {
		cp := *f
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

// ListActiveFlows returns flows that are not in a terminal state.
func (s *Store) ListActiveFlows() ([]*FlowRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*FlowRecord
	for _, f := range s.flows {
		if f.Status == FlowActive || f.Status == FlowBlocked {
			cp := *f
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

// DeleteFlow removes a flow by ID.
func (s *Store) DeleteFlow(flowID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.flows, flowID)
	s.markDirty()
	return nil
}

// --- Summary ---

// Summary returns aggregate statistics for the task ledger.
func (s *Store) Summary() (*RegistrySummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sum := &RegistrySummary{
		ByStatus:  make(map[TaskStatus]int),
		ByRuntime: make(map[TaskRuntime]int),
	}

	for _, t := range s.tasks {
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
	return sum, nil
}
