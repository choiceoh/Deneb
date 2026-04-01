package tasks

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS task_runs (
	task_id            TEXT PRIMARY KEY,
	runtime            TEXT NOT NULL,
	source_id          TEXT,
	owner_key          TEXT NOT NULL,
	scope_kind         TEXT NOT NULL DEFAULT 'session',
	child_session_key  TEXT,
	parent_task_id     TEXT,
	agent_id           TEXT,
	run_id             TEXT,
	label              TEXT,
	task               TEXT NOT NULL,
	status             TEXT NOT NULL,
	delivery_status    TEXT NOT NULL,
	notify_policy      TEXT NOT NULL,
	created_at         INTEGER NOT NULL,
	started_at         INTEGER,
	ended_at           INTEGER,
	last_event_at      INTEGER,
	cleanup_after      INTEGER,
	error              TEXT,
	progress_summary   TEXT,
	terminal_summary   TEXT,
	terminal_outcome   TEXT,
	flow_id            TEXT
);

CREATE INDEX IF NOT EXISTS idx_task_runs_run_id ON task_runs(run_id);
CREATE INDEX IF NOT EXISTS idx_task_runs_status ON task_runs(status);
CREATE INDEX IF NOT EXISTS idx_task_runs_runtime_status ON task_runs(runtime, status);
CREATE INDEX IF NOT EXISTS idx_task_runs_cleanup_after ON task_runs(cleanup_after);
CREATE INDEX IF NOT EXISTS idx_task_runs_last_event_at ON task_runs(last_event_at);
CREATE INDEX IF NOT EXISTS idx_task_runs_owner_key ON task_runs(owner_key);
CREATE INDEX IF NOT EXISTS idx_task_runs_child_session_key ON task_runs(child_session_key);
CREATE INDEX IF NOT EXISTS idx_task_runs_flow_id ON task_runs(flow_id);

CREATE TABLE IF NOT EXISTS task_events (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id  TEXT NOT NULL,
	at       INTEGER NOT NULL,
	kind     TEXT NOT NULL,
	summary  TEXT
);

CREATE INDEX IF NOT EXISTS idx_task_events_task_id ON task_events(task_id);

CREATE TABLE IF NOT EXISTS task_delivery_state (
	task_id                 TEXT PRIMARY KEY,
	requester_origin_json   TEXT,
	last_notified_event_at  INTEGER
);

CREATE TABLE IF NOT EXISTS flows (
	flow_id            TEXT PRIMARY KEY,
	label              TEXT NOT NULL,
	status             TEXT NOT NULL DEFAULT 'active',
	owner_key          TEXT NOT NULL,
	parent_session_key TEXT,
	created_at         INTEGER NOT NULL,
	updated_at         INTEGER NOT NULL,
	completed_at       INTEGER,
	error              TEXT,
	task_count         INTEGER NOT NULL DEFAULT 0,
	completed_count    INTEGER NOT NULL DEFAULT 0,
	failed_count       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_flows_status ON flows(status);
CREATE INDEX IF NOT EXISTS idx_flows_owner_key ON flows(owner_key);
`

// Store provides SQLite-backed persistence for the task ledger.
type Store struct {
	mu     sync.Mutex
	db     *sql.DB
	dbPath string
	logger *slog.Logger
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

// OpenStore opens or creates the task ledger database.
func OpenStore(cfg StoreConfig, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}

	dir := filepath.Dir(cfg.DatabasePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("tasks store: mkdir %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("tasks store: open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	// WAL mode for concurrent reads.
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("tasks store: %s: %w", pragma, err)
		}
	}

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("tasks store: init schema: %w", err)
	}

	return &Store{
		db:     db,
		dbPath: cfg.DatabasePath,
		logger: logger,
	}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

// --- Task CRUD ---

// UpsertTask inserts or updates a task record.
func (s *Store) UpsertTask(t *TaskRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO task_runs (
			task_id, runtime, source_id, owner_key, scope_kind,
			child_session_key, parent_task_id, agent_id, run_id, label,
			task, status, delivery_status, notify_policy,
			created_at, started_at, ended_at, last_event_at, cleanup_after,
			error, progress_summary, terminal_summary, terminal_outcome, flow_id
		) VALUES (?,?,?,?,?, ?,?,?,?,?, ?,?,?,?, ?,?,?,?,?, ?,?,?,?,?)
		ON CONFLICT(task_id) DO UPDATE SET
			runtime=excluded.runtime, source_id=excluded.source_id,
			owner_key=excluded.owner_key, scope_kind=excluded.scope_kind,
			child_session_key=excluded.child_session_key,
			parent_task_id=excluded.parent_task_id, agent_id=excluded.agent_id,
			run_id=excluded.run_id, label=excluded.label, task=excluded.task,
			status=excluded.status, delivery_status=excluded.delivery_status,
			notify_policy=excluded.notify_policy,
			created_at=excluded.created_at, started_at=excluded.started_at,
			ended_at=excluded.ended_at, last_event_at=excluded.last_event_at,
			cleanup_after=excluded.cleanup_after, error=excluded.error,
			progress_summary=excluded.progress_summary,
			terminal_summary=excluded.terminal_summary,
			terminal_outcome=excluded.terminal_outcome, flow_id=excluded.flow_id`,
		t.TaskID, t.Runtime, nullStr(t.SourceID), t.OwnerKey, t.ScopeKind,
		nullStr(t.ChildSessionKey), nullStr(t.ParentTaskID), nullStr(t.AgentID),
		nullStr(t.RunID), nullStr(t.Label),
		t.Task, t.Status, t.DeliveryStatus, t.NotifyPolicy,
		t.CreatedAt, nullInt(t.StartedAt), nullInt(t.EndedAt),
		nullInt(t.LastEventAt), nullInt(t.CleanupAfter),
		nullStr(t.Error), nullStr(t.ProgressSummary),
		nullStr(t.TerminalSummary), nullStr(string(t.TerminalOutcome)),
		nullStr(t.FlowID),
	)
	return err
}

// GetTask retrieves a single task by ID.
func (s *Store) GetTask(taskID string) (*TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`SELECT `+taskColumns+` FROM task_runs WHERE task_id = ?`, taskID)
	return scanTaskRecord(row)
}

// GetTaskByRunID retrieves a task by its run ID.
func (s *Store) GetTaskByRunID(runID string) (*TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`SELECT `+taskColumns+` FROM task_runs WHERE run_id = ? ORDER BY created_at DESC LIMIT 1`, runID)
	return scanTaskRecord(row)
}

// ListAll returns all tasks ordered by creation time.
func (s *Store) ListAll() ([]*TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.queryTasks(`SELECT `+taskColumns+` FROM task_runs ORDER BY created_at ASC`)
}

// ListByStatus returns tasks matching the given status.
func (s *Store) ListByStatus(status TaskStatus) ([]*TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.queryTasks(`SELECT `+taskColumns+` FROM task_runs WHERE status = ? ORDER BY created_at ASC`, status)
}

// ListActive returns all queued, running, or blocked tasks.
func (s *Store) ListActive() ([]*TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.queryTasks(`SELECT `+taskColumns+` FROM task_runs WHERE status IN ('queued','running','blocked') ORDER BY created_at ASC`)
}

// ListByOwner returns tasks for a specific owner key.
func (s *Store) ListByOwner(ownerKey string) ([]*TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.queryTasks(`SELECT `+taskColumns+` FROM task_runs WHERE owner_key = ? ORDER BY created_at ASC`, ownerKey)
}

// ListByFlowID returns all tasks belonging to a flow.
func (s *Store) ListByFlowID(flowID string) ([]*TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.queryTasks(`SELECT `+taskColumns+` FROM task_runs WHERE flow_id = ? ORDER BY created_at ASC`, flowID)
}

// ListByRuntime returns tasks for a specific runtime.
func (s *Store) ListByRuntime(runtime TaskRuntime) ([]*TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.queryTasks(`SELECT `+taskColumns+` FROM task_runs WHERE runtime = ? ORDER BY created_at ASC`, runtime)
}

// DeleteTask removes a task and its events.
func (s *Store) DeleteTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM task_events WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM task_delivery_state WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM task_runs WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteTerminalBefore removes terminal tasks older than the given timestamp.
func (s *Store) DeleteTerminalBefore(beforeMs int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Delete events for terminal tasks that are old enough.
	if _, err := tx.Exec(`
		DELETE FROM task_events WHERE task_id IN (
			SELECT task_id FROM task_runs
			WHERE status IN ('succeeded','failed','timed_out','cancelled','lost')
			AND cleanup_after IS NOT NULL AND cleanup_after < ?
		)`, beforeMs); err != nil {
		return 0, err
	}

	// Delete delivery states.
	if _, err := tx.Exec(`
		DELETE FROM task_delivery_state WHERE task_id IN (
			SELECT task_id FROM task_runs
			WHERE status IN ('succeeded','failed','timed_out','cancelled','lost')
			AND cleanup_after IS NOT NULL AND cleanup_after < ?
		)`, beforeMs); err != nil {
		return 0, err
	}

	res, err := tx.Exec(`
		DELETE FROM task_runs
		WHERE status IN ('succeeded','failed','timed_out','cancelled','lost')
		AND cleanup_after IS NOT NULL AND cleanup_after < ?`, beforeMs)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// --- Task Events ---

// AppendEvent records an audit trail entry.
func (s *Store) AppendEvent(evt *TaskEventRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`INSERT INTO task_events (task_id, at, kind, summary) VALUES (?,?,?,?)`,
		evt.TaskID, evt.At, evt.Kind, nullStr(evt.Summary))
	return err
}

// ListEvents returns all events for a task, ordered chronologically.
func (s *Store) ListEvents(taskID string) ([]*TaskEventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT task_id, at, kind, summary FROM task_events WHERE task_id = ? ORDER BY at ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*TaskEventRecord
	for rows.Next() {
		var e TaskEventRecord
		var summary sql.NullString
		if err := rows.Scan(&e.TaskID, &e.At, &e.Kind, &summary); err != nil {
			return nil, err
		}
		e.Summary = summary.String
		events = append(events, &e)
	}
	return events, rows.Err()
}

// --- Flow CRUD ---

// UpsertFlow inserts or updates a flow record.
func (s *Store) UpsertFlow(f *FlowRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO flows (
			flow_id, label, status, owner_key, parent_session_key,
			created_at, updated_at, completed_at, error,
			task_count, completed_count, failed_count
		) VALUES (?,?,?,?,?, ?,?,?,?, ?,?,?)
		ON CONFLICT(flow_id) DO UPDATE SET
			label=excluded.label, status=excluded.status,
			owner_key=excluded.owner_key, parent_session_key=excluded.parent_session_key,
			updated_at=excluded.updated_at, completed_at=excluded.completed_at,
			error=excluded.error,
			task_count=excluded.task_count, completed_count=excluded.completed_count,
			failed_count=excluded.failed_count`,
		f.FlowID, f.Label, f.Status, f.OwnerKey, nullStr(f.ParentSessionKey),
		f.CreatedAt, f.UpdatedAt, nullInt(f.CompletedAt), nullStr(f.Error),
		f.TaskCount, f.CompletedCount, f.FailedCount,
	)
	return err
}

// GetFlow retrieves a flow by ID.
func (s *Store) GetFlow(flowID string) (*FlowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`SELECT `+flowColumns+` FROM flows WHERE flow_id = ?`, flowID)
	return scanFlowRecord(row)
}

// ListFlows returns all flows ordered by creation time.
func (s *Store) ListFlows() ([]*FlowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT ` + flowColumns + ` FROM flows ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flows []*FlowRecord
	for rows.Next() {
		f, err := scanFlowFromRows(rows)
		if err != nil {
			return nil, err
		}
		flows = append(flows, f)
	}
	return flows, rows.Err()
}

// ListActiveFlows returns flows that are not in a terminal state.
func (s *Store) ListActiveFlows() ([]*FlowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT `+flowColumns+` FROM flows WHERE status IN ('active','blocked') ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flows []*FlowRecord
	for rows.Next() {
		f, err := scanFlowFromRows(rows)
		if err != nil {
			return nil, err
		}
		flows = append(flows, f)
	}
	return flows, rows.Err()
}

// DeleteFlow removes a flow by ID.
func (s *Store) DeleteFlow(flowID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM flows WHERE flow_id = ?`, flowID)
	return err
}

// --- Summary ---

// Summary returns aggregate statistics for the task ledger.
func (s *Store) Summary() (*RegistrySummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sum := &RegistrySummary{
		ByStatus:  make(map[TaskStatus]int),
		ByRuntime: make(map[TaskRuntime]int),
	}

	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM task_runs GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		st := TaskStatus(status)
		sum.ByStatus[st] = count
		sum.Total += count
		if st.IsActive() {
			sum.Active += count
		}
		if st.IsTerminal() {
			sum.Terminal += count
		}
		if st == StatusFailed || st == StatusTimedOut {
			sum.Failures += count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows2, err := s.db.Query(`SELECT runtime, COUNT(*) FROM task_runs GROUP BY runtime`)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var runtime string
		var count int
		if err := rows2.Scan(&runtime, &count); err != nil {
			return nil, err
		}
		sum.ByRuntime[TaskRuntime(runtime)] = count
	}
	return sum, rows2.Err()
}

// --- Helpers ---

const taskColumns = `task_id, runtime, source_id, owner_key, scope_kind,
	child_session_key, parent_task_id, agent_id, run_id, label,
	task, status, delivery_status, notify_policy,
	created_at, started_at, ended_at, last_event_at, cleanup_after,
	error, progress_summary, terminal_summary, terminal_outcome, flow_id`

const flowColumns = `flow_id, label, status, owner_key, parent_session_key,
	created_at, updated_at, completed_at, error,
	task_count, completed_count, failed_count`

type scanner interface {
	Scan(dest ...any) error
}

func scanTaskRecord(row scanner) (*TaskRecord, error) {
	var t TaskRecord
	var sourceID, childSess, parentTask, agentID, runID, label sql.NullString
	var errStr, progressSum, termSum, termOutcome, flowID sql.NullString
	var startedAt, endedAt, lastEvt, cleanupAfter sql.NullInt64

	err := row.Scan(
		&t.TaskID, &t.Runtime, &sourceID, &t.OwnerKey, &t.ScopeKind,
		&childSess, &parentTask, &agentID, &runID, &label,
		&t.Task, &t.Status, &t.DeliveryStatus, &t.NotifyPolicy,
		&t.CreatedAt, &startedAt, &endedAt, &lastEvt, &cleanupAfter,
		&errStr, &progressSum, &termSum, &termOutcome, &flowID,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	t.SourceID = sourceID.String
	t.ChildSessionKey = childSess.String
	t.ParentTaskID = parentTask.String
	t.AgentID = agentID.String
	t.RunID = runID.String
	t.Label = label.String
	t.StartedAt = startedAt.Int64
	t.EndedAt = endedAt.Int64
	t.LastEventAt = lastEvt.Int64
	t.CleanupAfter = cleanupAfter.Int64
	t.Error = errStr.String
	t.ProgressSummary = progressSum.String
	t.TerminalSummary = termSum.String
	t.TerminalOutcome = TerminalOutcome(termOutcome.String)
	t.FlowID = flowID.String

	return &t, nil
}

func scanFlowRecord(row scanner) (*FlowRecord, error) {
	var f FlowRecord
	var parentSess, errStr sql.NullString
	var completedAt sql.NullInt64

	err := row.Scan(
		&f.FlowID, &f.Label, &f.Status, &f.OwnerKey, &parentSess,
		&f.CreatedAt, &f.UpdatedAt, &completedAt, &errStr,
		&f.TaskCount, &f.CompletedCount, &f.FailedCount,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	f.ParentSessionKey = parentSess.String
	f.CompletedAt = completedAt.Int64
	f.Error = errStr.String
	return &f, nil
}

func scanFlowFromRows(rows *sql.Rows) (*FlowRecord, error) {
	var f FlowRecord
	var parentSess, errStr sql.NullString
	var completedAt sql.NullInt64

	err := rows.Scan(
		&f.FlowID, &f.Label, &f.Status, &f.OwnerKey, &parentSess,
		&f.CreatedAt, &f.UpdatedAt, &completedAt, &errStr,
		&f.TaskCount, &f.CompletedCount, &f.FailedCount,
	)
	if err != nil {
		return nil, err
	}

	f.ParentSessionKey = parentSess.String
	f.CompletedAt = completedAt.Int64
	f.Error = errStr.String
	return &f, nil
}

func (s *Store) queryTasks(query string, args ...any) ([]*TaskRecord, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*TaskRecord
	for rows.Next() {
		t, err := scanTaskRecord(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt(n int64) sql.NullInt64 {
	if n == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: n, Valid: true}
}
