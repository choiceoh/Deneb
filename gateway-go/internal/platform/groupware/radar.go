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
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
)

const (
	RadarTaskName              = "groupware-radar"
	DefaultRadarInterval       = 10 * time.Minute
	DefaultRadarMaxPerCycle    = 3
	DefaultRadarMaxEscalations = 3
	// DefaultRadarCCMaxPerCycle bounds 수신참조 ingests per cycle — the cc lane
	// is knowledge accumulation (not action relay), so it drains slower and
	// never competes with the pending lane for LLM budget.
	DefaultRadarCCMaxPerCycle = 2
	// radarCCRetention prunes cc tracking entries that fell off the list this
	// long ago. cc docs never "resolve" (no done reconciliation), so age is the
	// only exit.
	radarCCRetention               = 30 * 24 * time.Hour
	RadarEscalationLevelFourHours  = 1
	RadarEscalationLevelTwentyFour = 2
	// RadarListFailAlertAfter consecutive list failures surface one ops feed
	// card (then stay quiet until recovery) so Amaranth/auth outages aren't
	// only a journal line.
	RadarListFailAlertAfter = 3
	// radarRetryBackoffCap bounds the per-document retry backoff. A document
	// whose callback keeps failing is never tombstoned — it retries at most
	// once a day, so it self-heals when the upstream recovers.
	radarRetryBackoffCap    = 24 * time.Hour
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
	CCMaxPerCycle  int
	List           RadarListFunc
	Now            func() time.Time
	OnPending      func(context.Context, ApprovalSummary) error
	OnEscalated    func(context.Context, ApprovalSummary, int, time.Duration) error
	OnResolved     func(context.Context, ApprovalSummary) error
	// OnCCNew fires once per newly observed 수신참조 document (informational
	// lane: no escalation, no resolution). Nil disables cc scanning entirely —
	// the cc folder is not even listed.
	OnCCNew func(context.Context, ApprovalSummary) error
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
	ccMaxPerCycle  int
	list           RadarListFunc
	now            func() time.Time
	onPending      func(context.Context, ApprovalSummary) error
	onEscalated    func(context.Context, ApprovalSummary, int, time.Duration) error
	onResolved     func(context.Context, ApprovalSummary) error
	onCCNew        func(context.Context, ApprovalSummary) error
	onListFailed   func(ctx context.Context, folder string, streak int, err error) error

	// scanMu serializes scan cycles: the autonomous service already serializes
	// periodic Runs, but TriggerScan (phone-notification trigger) can arrive
	// concurrently. Held across the whole scan INCLUDING callbacks — callbacks
	// must never call back into the Radar (they don't: they read/analyze/post
	// feed cards).
	scanMu sync.Mutex
}

var _ autonomous.PeriodicTask = (*Radar)(nil)

type radarState struct {
	Docs            map[string]radarDocState `json:"docs"`
	LastPollAt      int64                    `json:"lastPollAt,omitempty"`
	ListFailStreak  int                      `json:"listFailStreak,omitempty"`
	ListFailAlerted bool                     `json:"listFailAlerted,omitempty"`
	LastListError   string                   `json:"lastListError,omitempty"`
	LastListFolder  string                   `json:"lastListFolder,omitempty"`
	// CCDocs tracks 수신참조 documents (informational lane — one-shot ingest,
	// no escalation/resolution). CCSeededAt marks the silent first contact:
	// the historical cc backlog is absorbed without callbacks so a cold start
	// never floods analysis or the feed.
	CCDocs     map[string]radarDocState `json:"ccDocs,omitempty"`
	CCSeededAt int64                    `json:"ccSeededAt,omitempty"`
}

type radarDocState struct {
	Fingerprint     string `json:"fingerprint"`
	Notified        bool   `json:"notified"`
	FirstSeenAt     int64  `json:"firstSeenAt,omitempty"`
	LastSeenAt      int64  `json:"lastSeenAt"`
	EscalationLevel int    `json:"escalationLevel,omitempty"`
	// FailCount/NextAttemptAt back off a document whose ingest callback keeps
	// failing. Without them an unreadable doc (deleted upstream, permission
	// drift) is retried every cycle forever and permanently occupies one of the
	// MaxPerCycle analysis slots. Both reset on success and on a fingerprint
	// change — a re-drafted document earns a fresh attempt.
	FailCount     int   `json:"failCount,omitempty"`
	NextAttemptAt int64 `json:"nextAttemptAt,omitempty"`
}

// retryDelay is the backoff before another attempt at a document whose callback
// has failed failCount times. The first failure retries on the very next cycle
// (most are transient); after that the delay doubles from the poll interval.
func (r *Radar) retryDelay(failCount int) time.Duration {
	if failCount <= 1 {
		return 0
	}
	delay := r.interval
	for range failCount - 2 {
		if delay >= radarRetryBackoffCap/2 {
			return radarRetryBackoffCap
		}
		delay *= 2
	}
	return min(delay, radarRetryBackoffCap)
}

// noteDocFailure records a failed ingest and schedules the next attempt.
func (r *Radar) noteDocFailure(docs map[string]radarDocState, id string, nowMs int64) time.Duration {
	stored := docs[id]
	stored.FailCount++
	delay := r.retryDelay(stored.FailCount)
	stored.NextAttemptAt = nowMs + delay.Milliseconds()
	docs[id] = stored
	return delay
}

// dueForAttempt reports whether a not-yet-notified document has served its
// backoff. Documents still backing off are not candidates at all, so they never
// consume a per-cycle slot.
func dueForAttempt(stored radarDocState, nowMs int64) bool {
	return !stored.Notified && nowMs >= stored.NextAttemptAt
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
	ccMaxPerCycle := cfg.CCMaxPerCycle
	if ccMaxPerCycle <= 0 {
		ccMaxPerCycle = DefaultRadarCCMaxPerCycle
	}
	return &Radar{
		reader:         cfg.Reader,
		statePath:      strings.TrimSpace(cfg.StatePath),
		interval:       interval,
		maxPerCycle:    maxPerCycle,
		maxEscalations: maxEscalations,
		ccMaxPerCycle:  ccMaxPerCycle,
		list:           list,
		now:            now,
		onPending:      cfg.OnPending,
		onEscalated:    cfg.OnEscalated,
		onResolved:     cfg.OnResolved,
		onCCNew:        cfg.OnCCNew,
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

// Run polls pending and done snapshots once (periodic cycle). Outside business
// hours it is a no-op — overnight/weekend approvals wait for the morning cycle
// or a phone-notification TriggerScan.
func (r *Radar) Run(ctx context.Context) error {
	if !IsRadarBusinessHours(r.now()) {
		return nil
	}
	return r.scan(ctx)
}

// TriggerScan runs one on-demand scan cycle immediately — the phone-notification
// trigger ("결재 도착" push proves the user cares right now), so it bypasses the
// business-hours gate. Same dedup as the periodic cycle: existing active cards
// and already-notified docs are skipped, so duplicate pushes are cheap no-ops.
func (r *Radar) TriggerScan(ctx context.Context) error {
	return r.scan(ctx)
}

// scan is the shared cycle body. scanMu serializes periodic and triggered scans.
func (r *Radar) scan(ctx context.Context) error {
	r.scanMu.Lock()
	defer r.scanMu.Unlock()

	now := r.now()
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
	doneByID := make(map[string]ApprovalSummary, len(done))
	for _, doc := range done {
		if id := strings.TrimSpace(doc.DocID); id != "" {
			doneByID[id] = doc
		}
	}

	candidates, pendingIDs := r.trackPending(&state, pending, nowMs)

	var runErrs []error
	runErrs = append(runErrs, r.notifyPending(ctx, &state, candidates, nowMs)...)
	runErrs = append(runErrs, r.escalatePending(ctx, &state, pending, nowMs)...)
	runErrs = append(runErrs, r.reconcileResolved(ctx, &state, pendingIDs, doneByID)...)

	// 수신참조 lane last: a cc hiccup must never starve the action lane, and
	// the pending/done work above is already reflected in state even when cc
	// listing fails.
	if r.onCCNew != nil {
		runErrs = append(runErrs, r.scanCC(ctx, &state, nowMs)...)
	}

	if err := saveRadarState(r.statePath, state); err != nil {
		runErrs = append(runErrs, err)
	}
	return errors.Join(runErrs...)
}

// trackPending folds this cycle's pending listing into durable per-doc state and
// returns the docs due for an ingest attempt plus the set of still-pending ids
// (the resolution pass needs it to tell "left the folder" from "never listed").
func (r *Radar) trackPending(
	state *radarState,
	pending []ApprovalSummary,
	nowMs int64,
) ([]ApprovalSummary, map[string]struct{}) {
	pendingIDs := make(map[string]struct{}, len(pending))
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
			// A re-drafted document deserves a clean attempt, not the old
			// document's backoff.
			stored.FailCount = 0
			stored.NextAttemptAt = 0
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
		if dueForAttempt(stored, nowMs) {
			candidates = append(candidates, doc)
		}
	}
	return candidates, pendingIDs
}

// notifyPending fires the pending callback for up to MaxPerCycle due documents,
// arming the retry backoff on failure so one unreadable doc cannot own a slot.
func (r *Radar) notifyPending(
	ctx context.Context,
	state *radarState,
	candidates []ApprovalSummary,
	nowMs int64,
) []error {
	var errs []error
	for i, doc := range candidates {
		if i >= r.maxPerCycle {
			break
		}
		if r.onPending == nil {
			errs = append(errs, fmt.Errorf("pending approval %s: callback unavailable", doc.DocID))
			continue
		}
		if err := r.onPending(ctx, doc); err != nil {
			delay := r.noteDocFailure(state.Docs, doc.DocID, nowMs)
			errs = append(errs, fmt.Errorf(
				"pending approval %s (attempt %d, retry in %s): %w",
				doc.DocID, state.Docs[doc.DocID].FailCount, delay, err,
			))
			continue
		}
		stored := state.Docs[doc.DocID]
		stored.Notified = true
		stored.FailCount = 0
		stored.NextAttemptAt = 0
		state.Docs[doc.DocID] = stored
	}
	return errs
}

// escalatePending raises an already-notified card at most once per threshold,
// without spending another LLM turn.
func (r *Radar) escalatePending(
	ctx context.Context,
	state *radarState,
	pending []ApprovalSummary,
	nowMs int64,
) []error {
	var errs []error
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
			errs = append(errs, fmt.Errorf("escalate approval %s: callback unavailable", doc.DocID))
			continue
		}
		if err := r.onEscalated(ctx, doc, level, age); err != nil {
			errs = append(errs, fmt.Errorf("escalate approval %s: %w", doc.DocID, err))
			continue
		}
		stored.EscalationLevel = level
		state.Docs[doc.DocID] = stored
		escalated++
	}
	return errs
}

// reconcileResolved drops tracking only for docs positively observed in the done
// folder — absence from pending alone is never proof of resolution.
func (r *Radar) reconcileResolved(
	ctx context.Context,
	state *radarState,
	pendingIDs map[string]struct{},
	doneByID map[string]ApprovalSummary,
) []error {
	trackedIDs := make([]string, 0, len(state.Docs))
	for id := range state.Docs {
		trackedIDs = append(trackedIDs, id)
	}
	sort.Strings(trackedIDs)
	var errs []error
	for _, id := range trackedIDs {
		if _, stillPending := pendingIDs[id]; stillPending {
			continue
		}
		doneDoc, positivelyDone := doneByID[id]
		if !positivelyDone {
			continue
		}
		if r.onResolved == nil {
			errs = append(errs, fmt.Errorf("resolved approval %s: callback unavailable", id))
			continue
		}
		if err := r.onResolved(ctx, doneDoc); err != nil {
			errs = append(errs, fmt.Errorf("resolved approval %s: %w", id, err))
			continue
		}
		delete(state.Docs, id)
	}
	return errs
}

// scanCC diffs the 수신참조 folder and fires OnCCNew once per newly observed
// document. cc documents are informational (someone else's approval line): no
// escalation, no done reconciliation, and fingerprint churn — status moving as
// other approvers act — never re-triggers ingestion.
func (r *Radar) scanCC(ctx context.Context, state *radarState, nowMs int64) []error {
	ccDocs, err := r.list(ctx, r.reader, "cc", radarApprovalListLimit)
	if err != nil {
		return []error{fmt.Errorf("list cc approvals: %w", err)}
	}
	if state.CCDocs == nil {
		state.CCDocs = make(map[string]radarDocState)
	}
	sortApprovalSummaries(ccDocs)
	// First contact absorbs the historical backlog silently.
	seeding := state.CCSeededAt == 0
	if seeding {
		state.CCSeededAt = nowMs
	}
	var errs []error
	seen := make(map[string]struct{}, len(ccDocs))
	attempted := 0
	for _, doc := range ccDocs {
		id := strings.TrimSpace(doc.DocID)
		if id == "" {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		stored, exists := state.CCDocs[id]
		if !exists {
			stored.Fingerprint = approvalFingerprint(doc)
			stored.FirstSeenAt = nowMs
			stored.Notified = seeding
		}
		stored.LastSeenAt = nowMs
		if dueForAttempt(stored, nowMs) && attempted < r.ccMaxPerCycle {
			attempted++
			if err := r.onCCNew(ctx, doc); err != nil {
				state.CCDocs[id] = stored
				delay := r.noteDocFailure(state.CCDocs, id, nowMs)
				stored = state.CCDocs[id]
				errs = append(errs, fmt.Errorf(
					"cc approval %s (attempt %d, retry in %s): %w",
					id, stored.FailCount, delay, err,
				))
			} else {
				stored.Notified = true
				stored.FailCount = 0
				stored.NextAttemptAt = 0
			}
		}
		state.CCDocs[id] = stored
	}
	for id, stored := range state.CCDocs {
		if _, still := seen[id]; still {
			continue
		}
		if nowMs-stored.LastSeenAt > radarCCRetention.Milliseconds() {
			delete(state.CCDocs, id)
		}
	}
	return errs
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
