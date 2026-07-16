package groupware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
)

const (
	RadarTaskName                  = "groupware-radar"
	DefaultRadarInterval           = 10 * time.Minute
	DefaultRadarMaxPerCycle        = 3
	DefaultRadarMaxEscalations     = 3
	RadarEscalationLevelFourHours  = 1
	RadarEscalationLevelTwentyFour = 2
	// RadarListFailAlertAfter consecutive list failures surface one ops feed
	// card (then stay quiet until recovery) so Amaranth/auth outages aren't
	// only a journal line.
	RadarListFailAlertAfter = 3
	radarApprovalListLimit  = 50
	radarIntervalMinutesEnv = "DENEB_GROUPWARE_RADAR_INTERVAL_MINUTES"
	// RadarListFailRefID is the durable work-feed ref for list-failure alerts.
	RadarListFailRefID = "groupware-radar-list"
)

var radarKST = time.FixedZone("KST", 9*60*60)

// RadarListFunc is the structured approval-list boundary used by Radar.
type RadarListFunc func(context.Context, Config, string, int) ([]ApprovalSummary, error)

// RadarConfig supplies the reader, durable state, callbacks, and test seams.
type RadarConfig struct {
	Reader         Config
	StatePath      string
	Interval       time.Duration
	MaxPerCycle    int
	MaxEscalations int
	List           RadarListFunc
	Now            func() time.Time
	OnPending      func(context.Context, ApprovalSummary) error
	OnEscalated    func(context.Context, ApprovalSummary, int, time.Duration) error
	OnResolved     func(context.Context, ApprovalSummary) error
	// OnListFailed fires once when list pending/done has failed
	// RadarListFailAlertAfter times in a row. Nil disables the alert.
	OnListFailed func(ctx context.Context, folder string, streak int, err error) error
}

// Radar deterministically diffs pending approval snapshots and reconciles cards
// only after the same docId is positively observed in the done folder.
type Radar struct {
	reader         Config
	statePath      string
	interval       time.Duration
	maxPerCycle    int
	maxEscalations int
	list           RadarListFunc
	now            func() time.Time
	onPending      func(context.Context, ApprovalSummary) error
	onEscalated    func(context.Context, ApprovalSummary, int, time.Duration) error
	onResolved     func(context.Context, ApprovalSummary) error
	onListFailed   func(ctx context.Context, folder string, streak int, err error) error
}

var _ autonomous.PeriodicTask = (*Radar)(nil)

type radarState struct {
	Docs            map[string]radarDocState `json:"docs"`
	LastPollAt      int64                    `json:"lastPollAt,omitempty"`
	ListFailStreak  int                      `json:"listFailStreak,omitempty"`
	ListFailAlerted bool                     `json:"listFailAlerted,omitempty"`
	LastListError   string                   `json:"lastListError,omitempty"`
	LastListFolder  string                   `json:"lastListFolder,omitempty"`
}

type radarDocState struct {
	Fingerprint     string `json:"fingerprint"`
	Notified        bool   `json:"notified"`
	FirstSeenAt     int64  `json:"firstSeenAt,omitempty"`
	LastSeenAt      int64  `json:"lastSeenAt"`
	EscalationLevel int    `json:"escalationLevel,omitempty"`
}

// NewRadar constructs a serial periodic approval radar.
func NewRadar(cfg RadarConfig) *Radar {
	interval := cfg.Interval
	if interval <= 0 {
		interval = radarIntervalFromEnv()
	}
	maxPerCycle := cfg.MaxPerCycle
	if maxPerCycle <= 0 {
		maxPerCycle = DefaultRadarMaxPerCycle
	}
	maxEscalations := cfg.MaxEscalations
	if maxEscalations <= 0 {
		maxEscalations = DefaultRadarMaxEscalations
	}
	list := cfg.List
	if list == nil {
		list = ListApprovals
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Radar{
		reader:         cfg.Reader,
		statePath:      strings.TrimSpace(cfg.StatePath),
		interval:       interval,
		maxPerCycle:    maxPerCycle,
		maxEscalations: maxEscalations,
		list:           list,
		now:            now,
		onPending:      cfg.OnPending,
		onEscalated:    cfg.OnEscalated,
		onResolved:     cfg.OnResolved,
		onListFailed:   cfg.OnListFailed,
	}
}

func (r *Radar) Name() string { return RadarTaskName }

func (r *Radar) Interval() time.Duration { return r.interval }

// IsRadarBusinessHours reports whether at is a weekday from 08:30 inclusive to
// 19:00 exclusive in Korea Standard Time.
func IsRadarBusinessHours(at time.Time) bool {
	kst := at.In(radarKST)
	if kst.Weekday() == time.Saturday || kst.Weekday() == time.Sunday {
		return false
	}
	minute := kst.Hour()*60 + kst.Minute()
	return minute >= 8*60+30 && minute < 19*60
}

// Run polls pending and done snapshots once. The autonomous service serializes
// task cycles, so Radar intentionally has no mutex and never holds a lock across
// callbacks.
func (r *Radar) Run(ctx context.Context) error {
	now := r.now()
	if !IsRadarBusinessHours(now) {
		return nil
	}
	if r.statePath == "" {
		return errors.New("groupware radar state path is required")
	}

	state, err := loadRadarState(r.statePath)
	if err != nil {
		return err
	}
	pending, err := r.list(ctx, r.reader, "pending", radarApprovalListLimit)
	if err != nil {
		return r.noteListFailure(ctx, state, "pending", err)
	}
	done, err := r.list(ctx, r.reader, "done", radarApprovalListLimit)
	if err != nil {
		return r.noteListFailure(ctx, state, "done", err)
	}
	r.clearListFailure(&state)

	sortApprovalSummaries(pending)
	sortApprovalSummaries(done)
	nowMs := now.UnixMilli()
	state.LastPollAt = nowMs
	pendingIDs := make(map[string]struct{}, len(pending))
	doneByID := make(map[string]ApprovalSummary, len(done))
	for _, doc := range done {
		if id := strings.TrimSpace(doc.DocID); id != "" {
			doneByID[id] = doc
		}
	}

	candidates := make([]ApprovalSummary, 0, len(pending))
	for _, doc := range pending {
		id := strings.TrimSpace(doc.DocID)
		if id == "" {
			continue
		}
		if _, duplicate := pendingIDs[id]; duplicate {
			continue
		}
		pendingIDs[id] = struct{}{}
		fingerprint := approvalFingerprint(doc)
		stored, exists := state.Docs[id]
		if !exists || stored.Fingerprint != fingerprint {
			stored.Fingerprint = fingerprint
			stored.Notified = false
		}
		if stored.FirstSeenAt == 0 {
			if stored.LastSeenAt > 0 {
				stored.FirstSeenAt = stored.LastSeenAt
			} else {
				stored.FirstSeenAt = nowMs
			}
		}
		stored.LastSeenAt = nowMs
		state.Docs[id] = stored
		if !stored.Notified {
			candidates = append(candidates, doc)
		}
	}

	var runErrs []error
	for i, doc := range candidates {
		if i >= r.maxPerCycle {
			break
		}
		if r.onPending == nil {
			runErrs = append(runErrs, fmt.Errorf("pending approval %s: callback unavailable", doc.DocID))
			continue
		}
		if err := r.onPending(ctx, doc); err != nil {
			runErrs = append(runErrs, fmt.Errorf("pending approval %s: %w", doc.DocID, err))
			continue
		}
		stored := state.Docs[doc.DocID]
		stored.Notified = true
		state.Docs[doc.DocID] = stored
	}

	// Escalate an existing card at most once per threshold without another LLM turn.
	escalated := 0
	for _, doc := range pending {
		if escalated >= r.maxEscalations {
			break
		}
		stored := state.Docs[doc.DocID]
		if !stored.Notified || stored.FirstSeenAt == 0 {
			continue
		}
		age := time.Duration(nowMs-stored.FirstSeenAt) * time.Millisecond
		level := radarEscalationLevel(age)
		if level <= stored.EscalationLevel {
			continue
		}
		if r.onEscalated == nil {
			runErrs = append(runErrs, fmt.Errorf("escalate approval %s: callback unavailable", doc.DocID))
			continue
		}
		if err := r.onEscalated(ctx, doc, level, age); err != nil {
			runErrs = append(runErrs, fmt.Errorf("escalate approval %s: %w", doc.DocID, err))
			continue
		}
		stored.EscalationLevel = level
		state.Docs[doc.DocID] = stored
		escalated++
	}

	trackedIDs := make([]string, 0, len(state.Docs))
	for id := range state.Docs {
		trackedIDs = append(trackedIDs, id)
	}
	sort.Strings(trackedIDs)
	for _, id := range trackedIDs {
		if _, stillPending := pendingIDs[id]; stillPending {
			continue
		}
		doneDoc, positivelyDone := doneByID[id]
		if !positivelyDone {
			continue
		}
		if r.onResolved == nil {
			runErrs = append(runErrs, fmt.Errorf("resolved approval %s: callback unavailable", id))
			continue
		}
		if err := r.onResolved(ctx, doneDoc); err != nil {
			runErrs = append(runErrs, fmt.Errorf("resolved approval %s: %w", id, err))
			continue
		}
		delete(state.Docs, id)
	}

	if err := saveRadarState(r.statePath, state); err != nil {
		runErrs = append(runErrs, err)
	}
	return errors.Join(runErrs...)
}

func (r *Radar) noteListFailure(ctx context.Context, state radarState, folder string, listErr error) error {
	wrapped := fmt.Errorf("list %s approvals: %w", folder, listErr)
	state.ListFailStreak++
	state.LastListError = wrapped.Error()
	state.LastListFolder = folder
	shouldAlert := state.ListFailStreak >= RadarListFailAlertAfter && !state.ListFailAlerted && r.onListFailed != nil
	if shouldAlert {
		if alertErr := r.onListFailed(ctx, folder, state.ListFailStreak, wrapped); alertErr != nil {
			_ = saveRadarState(r.statePath, state)
			return errors.Join(wrapped, fmt.Errorf("list-failure alert: %w", alertErr))
		}
		state.ListFailAlerted = true
	}
	if saveErr := saveRadarState(r.statePath, state); saveErr != nil {
		return errors.Join(wrapped, saveErr)
	}
	return wrapped
}

func (r *Radar) clearListFailure(state *radarState) {
	if state == nil {
		return
	}
	state.ListFailStreak = 0
	state.ListFailAlerted = false
	state.LastListError = ""
	state.LastListFolder = ""
}

func radarEscalationLevel(age time.Duration) int {
	switch {
	case age >= 24*time.Hour:
		return RadarEscalationLevelTwentyFour
	case age >= 4*time.Hour:
		return RadarEscalationLevelFourHours
	default:
		return 0
	}
}

func radarIntervalFromEnv() time.Duration {
	minutes, err := strconv.Atoi(strings.TrimSpace(os.Getenv(radarIntervalMinutesEnv)))
	if err == nil && minutes > 0 {
		return time.Duration(minutes) * time.Minute
	}
	return DefaultRadarInterval
}

func sortApprovalSummaries(docs []ApprovalSummary) {
	sort.SliceStable(docs, func(i, j int) bool {
		return strings.TrimSpace(docs[i].DocID) < strings.TrimSpace(docs[j].DocID)
	})
}

func approvalFingerprint(doc ApprovalSummary) string {
	raw := strings.Join([]string{
		strings.TrimSpace(doc.DocID),
		strings.TrimSpace(doc.Title),
		strings.TrimSpace(doc.DocNo),
		strings.TrimSpace(doc.Drafter),
		strings.TrimSpace(doc.Date),
		strings.TrimSpace(doc.Status),
		strings.TrimSpace(doc.Folder),
	}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func loadRadarState(path string) (radarState, error) {
	state := radarState{Docs: make(map[string]radarDocState)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return radarState{}, fmt.Errorf("read groupware radar state: %w", err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return radarState{}, fmt.Errorf("parse groupware radar state: %w", err)
	}
	if state.Docs == nil {
		state.Docs = make(map[string]radarDocState)
	}
	return state, nil
}

func saveRadarState(path string, state radarState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create groupware radar state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal groupware radar state: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".groupware-radar-*.tmp")
	if err != nil {
		return fmt.Errorf("create groupware radar temp state: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod groupware radar temp state: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write groupware radar temp state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync groupware radar temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close groupware radar temp state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace groupware radar state: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod groupware radar state: %w", err)
	}
	return nil
}
