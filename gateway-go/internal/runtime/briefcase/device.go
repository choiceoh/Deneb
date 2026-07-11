package briefcase

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
)

const (
	maxDevicePlanSourceBytes = 4 << 20
	maxDevicePlanItems       = 256
	maxDevicePlanFieldBytes  = 256 << 10
	maxDeviceFailureBytes    = 8 << 10
)

var (
	ErrInvalidDevicePlan = errors.New("invalid briefcase device plan")
	ErrUnplannedAction   = errors.New("briefcase device action is not planned")
	ErrActionConflict    = errors.New("briefcase device action id conflicts with prior request")
)

type DeviceStatus string

const (
	DeviceConfirmed   DeviceStatus = "confirmed"
	DeviceFailed      DeviceStatus = "failed"
	DeviceUnconfirmed DeviceStatus = "unconfirmed"
	DeviceDelayed     DeviceStatus = "delayed"
)

// DevicePlan scripts the only action payload accepted for ActionID. A delayed
// plan becomes FinalStatus after Delay; all other statuses are immediate.
type DevicePlan struct {
	ActionID    string          `json:"actionId"`
	Kind        string          `json:"kind"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Status      DeviceStatus    `json:"status"`
	Delay       time.Duration   `json:"delay,omitempty"`
	FinalStatus DeviceStatus    `json:"finalStatus,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Failure     string          `json:"failure,omitempty"`
}

type devicePlanSourceDocument struct {
	Plans []devicePlanSourceItem `json:"plans"`
}

type devicePlanSourceItem struct {
	ActionID     string          `json:"actionId"`
	Kind         string          `json:"kind"`
	Payload      json.RawMessage `json:"payload"`
	Status       DeviceStatus    `json:"status"`
	DelaySeconds int64           `json:"delaySeconds,omitempty"`
	FinalStatus  DeviceStatus    `json:"finalStatus,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
	Failure      string          `json:"failure,omitempty"`
}

// DecodeDevicePlanSource strictly decodes the signed wire document used by the
// CLI and harness. The harness, not its caller, turns authenticated bytes into
// executable Device Twin behavior.
func DecodeDevicePlanSource(data []byte) ([]DevicePlan, error) {
	if len(data) == 0 || len(data) > maxDevicePlanSourceBytes {
		return nil, fmt.Errorf("%w: device plan source is missing or oversized", ErrInvalidDevicePlan)
	}
	if err := casepack.RejectDuplicateJSONKeys(data); err != nil {
		return nil, fmt.Errorf("%w: ambiguous device plan source: %w", ErrInvalidDevicePlan, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document devicePlanSourceDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%w: decode source: %w", ErrInvalidDevicePlan, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("%w: multiple JSON values", ErrInvalidDevicePlan)
		}
		return nil, fmt.Errorf("%w: trailing source data: %w", ErrInvalidDevicePlan, err)
	}
	if len(document.Plans) == 0 {
		return nil, fmt.Errorf("%w: source contains no plans", ErrInvalidDevicePlan)
	}
	if len(document.Plans) > maxDevicePlanItems {
		return nil, fmt.Errorf("%w: source exceeds %d plans", ErrInvalidDevicePlan, maxDevicePlanItems)
	}
	plans := make([]DevicePlan, 0, len(document.Plans))
	for _, item := range document.Plans {
		if len(item.Payload) > maxDevicePlanFieldBytes || len(item.Result) > maxDevicePlanFieldBytes || len(item.Failure) > maxDeviceFailureBytes {
			return nil, fmt.Errorf("%w: action %q payload, result, or failure is oversized", ErrInvalidDevicePlan, item.ActionID)
		}
		if item.DelaySeconds < 0 || item.DelaySeconds > int64(time.Duration(1<<63-1)/time.Second) {
			return nil, fmt.Errorf("%w: action %q has invalid delaySeconds", ErrInvalidDevicePlan, item.ActionID)
		}
		plans = append(plans, DevicePlan{
			ActionID: item.ActionID, Kind: item.Kind, Payload: item.Payload,
			Status: item.Status, Delay: time.Duration(item.DelaySeconds) * time.Second,
			FinalStatus: item.FinalStatus, Result: item.Result, Failure: item.Failure,
		})
	}
	return plans, nil
}

type DeviceAction struct {
	ActionID string          `json:"actionId"`
	Kind     string          `json:"kind"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

type DeviceActionRecord struct {
	ActionID          string          `json:"actionId"`
	Kind              string          `json:"kind"`
	Payload           json.RawMessage `json:"payload"`
	Fingerprint       string          `json:"fingerprint"`
	Status            DeviceStatus    `json:"status"`
	RequestedAt       time.Time       `json:"requestedAt"`
	ReadyAt           time.Time       `json:"readyAt,omitempty"`
	ResolvedAt        time.Time       `json:"resolvedAt,omitempty"`
	Result            json.RawMessage `json:"result,omitempty"`
	Failure           string          `json:"failure,omitempty"`
	DuplicateAttempts int             `json:"duplicateAttempts,omitempty"`
}

type DeviceActionResult struct {
	Record    DeviceActionRecord `json:"record"`
	Duplicate bool               `json:"duplicate"`
}

// DevicePlansDigest returns a stable semantic digest independent of source JSON
// whitespace, object key order, and plan ordering.
func DevicePlansDigest(plans []DevicePlan) (string, error) {
	type wirePlan struct {
		ActionID    string          `json:"actionId"`
		Kind        string          `json:"kind"`
		Payload     json.RawMessage `json:"payload"`
		Status      DeviceStatus    `json:"status"`
		DelayNanos  int64           `json:"delayNanos,omitempty"`
		FinalStatus DeviceStatus    `json:"finalStatus,omitempty"`
		Result      json.RawMessage `json:"result,omitempty"`
		Failure     string          `json:"failure,omitempty"`
	}
	wire := make([]wirePlan, 0, len(plans))
	for _, plan := range plans {
		payload, err := canonicalJSON(plan.Payload)
		if err != nil {
			return "", fmt.Errorf("%w: action %q payload: %w", ErrInvalidDevicePlan, plan.ActionID, err)
		}
		result, err := canonicalJSON(plan.Result)
		if err != nil {
			return "", fmt.Errorf("%w: action %q result: %w", ErrInvalidDevicePlan, plan.ActionID, err)
		}
		if len(bytes.TrimSpace(plan.Result)) == 0 {
			result = nil
		}
		wire = append(wire, wirePlan{
			ActionID: strings.TrimSpace(plan.ActionID), Kind: strings.TrimSpace(plan.Kind),
			Payload: payload, Status: plan.Status, DelayNanos: int64(plan.Delay),
			FinalStatus: plan.FinalStatus, Result: result, Failure: plan.Failure,
		})
	}
	sort.Slice(wire, func(i, j int) bool { return wire[i].ActionID < wire[j].ActionID })
	data, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("%w: encode plans: %w", ErrInvalidDevicePlan, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

type preparedDevicePlan struct {
	plan        DevicePlan
	fingerprint string
}

// DeviceTwin is a fully scripted device with an append-only action ledger. It
// performs no I/O. Unknown actions fail closed, and reusing an action ID never
// applies the action twice.
type DeviceTwin struct {
	mu     sync.Mutex
	clock  Clock
	plans  map[string]preparedDevicePlan
	ledger map[string]DeviceActionRecord
	order  []string
}

func NewDeviceTwin(clock Clock, plans []DevicePlan) (*DeviceTwin, error) {
	if clock == nil {
		return nil, fmt.Errorf("%w: clock is required", ErrInvalidDevicePlan)
	}
	prepared := make(map[string]preparedDevicePlan, len(plans))
	for i, plan := range plans {
		plan.ActionID = strings.TrimSpace(plan.ActionID)
		plan.Kind = strings.TrimSpace(plan.Kind)
		if plan.ActionID == "" || plan.Kind == "" {
			return nil, fmt.Errorf("%w: plan %d needs action id and kind", ErrInvalidDevicePlan, i)
		}
		if _, exists := prepared[plan.ActionID]; exists {
			return nil, fmt.Errorf("%w: duplicate action id %q", ErrInvalidDevicePlan, plan.ActionID)
		}
		payload, err := canonicalJSON(plan.Payload)
		if err != nil {
			return nil, fmt.Errorf("%w: action %q payload: %w", ErrInvalidDevicePlan, plan.ActionID, err)
		}
		result, err := canonicalJSON(plan.Result)
		if err != nil {
			return nil, fmt.Errorf("%w: action %q result: %w", ErrInvalidDevicePlan, plan.ActionID, err)
		}
		if len(bytes.TrimSpace(plan.Result)) == 0 {
			result = nil
		}
		plan.Payload = payload
		plan.Result = result
		if err := validateDevicePlan(plan); err != nil {
			return nil, err
		}
		prepared[plan.ActionID] = preparedDevicePlan{
			plan:        cloneDevicePlan(plan),
			fingerprint: deviceFingerprint(plan.Kind, payload),
		}
	}
	return &DeviceTwin{
		clock:  clock,
		plans:  prepared,
		ledger: make(map[string]DeviceActionRecord),
	}, nil
}

func validateDevicePlan(plan DevicePlan) error {
	switch plan.Status {
	case DeviceConfirmed, DeviceFailed, DeviceUnconfirmed:
		if plan.Delay != 0 || plan.FinalStatus != "" {
			return fmt.Errorf("%w: immediate action %q cannot have delay/final status", ErrInvalidDevicePlan, plan.ActionID)
		}
	case DeviceDelayed:
		if plan.Delay <= 0 {
			return fmt.Errorf("%w: delayed action %q needs a positive delay", ErrInvalidDevicePlan, plan.ActionID)
		}
		if !isFinalDeviceStatus(plan.FinalStatus) {
			return fmt.Errorf("%w: delayed action %q needs a final status", ErrInvalidDevicePlan, plan.ActionID)
		}
	default:
		return fmt.Errorf("%w: action %q has unknown status %q", ErrInvalidDevicePlan, plan.ActionID, plan.Status)
	}
	return nil
}

func isFinalDeviceStatus(status DeviceStatus) bool {
	return status == DeviceConfirmed || status == DeviceFailed || status == DeviceUnconfirmed
}

// Perform records the scripted outcome for action. The same ActionID and
// payload returns the prior ledger entry with Duplicate=true. Reusing an ID for
// a different action is an error and leaves the original record untouched.
func (d *DeviceTwin) Perform(action DeviceAction) (DeviceActionResult, error) {
	return d.PerformContext(context.Background(), action)
}

// PerformContext is Perform with cooperative cancellation. Cancellation is
// checked again while the ledger lock is held, immediately before any ledger
// mutation, so a pre-canceled tool call cannot consume an action.
func (d *DeviceTwin) PerformContext(ctx context.Context, action DeviceAction) (DeviceActionResult, error) {
	if d == nil {
		return DeviceActionResult{}, ErrUnplannedAction
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return DeviceActionResult{}, err
	}
	action.ActionID = strings.TrimSpace(action.ActionID)
	action.Kind = strings.TrimSpace(action.Kind)
	if action.ActionID == "" || action.Kind == "" {
		return DeviceActionResult{}, fmt.Errorf("%w: action id and kind are required", ErrUnplannedAction)
	}
	payload, err := canonicalJSON(action.Payload)
	if err != nil {
		return DeviceActionResult{}, fmt.Errorf("%w: invalid action payload: %w", ErrUnplannedAction, err)
	}
	fingerprint := deviceFingerprint(action.Kind, payload)
	if err := ctx.Err(); err != nil {
		return DeviceActionResult{}, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return DeviceActionResult{}, err
	}
	if existing, ok := d.ledger[action.ActionID]; ok {
		if existing.Fingerprint != fingerprint {
			return DeviceActionResult{}, fmt.Errorf("%w: %q", ErrActionConflict, action.ActionID)
		}
		existing.DuplicateAttempts++
		d.ledger[action.ActionID] = existing
		return DeviceActionResult{Record: cloneDeviceRecord(existing), Duplicate: true}, nil
	}
	prepared, ok := d.plans[action.ActionID]
	if !ok || prepared.fingerprint != fingerprint {
		return DeviceActionResult{}, fmt.Errorf("%w: %q", ErrUnplannedAction, action.ActionID)
	}
	now := canonicalTime(d.clock.Now())
	record := DeviceActionRecord{
		ActionID:    action.ActionID,
		Kind:        action.Kind,
		Payload:     bytes.Clone(payload),
		Fingerprint: fingerprint,
		Status:      prepared.plan.Status,
		RequestedAt: now,
	}
	if prepared.plan.Status == DeviceDelayed {
		record.ReadyAt = now.Add(prepared.plan.Delay)
	} else {
		record.ResolvedAt = now
		record.Result = bytes.Clone(prepared.plan.Result)
		record.Failure = prepared.plan.Failure
	}
	d.ledger[action.ActionID] = record
	d.order = append(d.order, action.ActionID)
	return DeviceActionResult{Record: cloneDeviceRecord(record)}, nil
}

// SettleDue deterministically resolves delayed actions whose ReadyAt has passed.
// ResolvedAt is ReadyAt rather than polling time, so replay results do not depend
// on how often the harness calls SettleDue.
func (d *DeviceTwin) SettleDue() []DeviceActionRecord {
	settled, _ := d.SettleDueContext(context.Background())
	return settled
}

// SettleDueContext computes all due transitions first and commits them only if
// the complete scan finishes before cancellation.
func (d *DeviceTwin) SettleDueContext(ctx context.Context) ([]DeviceActionRecord, error) {
	if d == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := canonicalTime(d.clock.Now())
	type pendingSettlement struct {
		id     string
		record DeviceActionRecord
	}
	var pending []pendingSettlement
	for _, id := range d.order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record := d.ledger[id]
		if record.Status != DeviceDelayed || record.ReadyAt.After(now) {
			continue
		}
		plan := d.plans[id].plan
		record.Status = plan.FinalStatus
		record.ResolvedAt = record.ReadyAt
		record.Result = bytes.Clone(plan.Result)
		record.Failure = plan.Failure
		pending = append(pending, pendingSettlement{id: id, record: record})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	settled := make([]DeviceActionRecord, 0, len(pending))
	for _, update := range pending {
		d.ledger[update.id] = update.record
		settled = append(settled, cloneDeviceRecord(update.record))
	}
	return settled, nil
}

func (d *DeviceTwin) Ledger() []DeviceActionRecord {
	ledger, _ := d.LedgerContext(context.Background())
	return ledger
}

func (d *DeviceTwin) LedgerContext(ctx context.Context) ([]DeviceActionRecord, error) {
	if d == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]DeviceActionRecord, 0, len(d.order))
	for _, id := range d.order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out = append(out, cloneDeviceRecord(d.ledger[id]))
	}
	return out, ctx.Err()
}

func cloneDevicePlan(plan DevicePlan) DevicePlan {
	plan.Payload = bytes.Clone(plan.Payload)
	plan.Result = bytes.Clone(plan.Result)
	return plan
}

func cloneDeviceRecord(record DeviceActionRecord) DeviceActionRecord {
	record.Payload = bytes.Clone(record.Payload)
	record.Result = bytes.Clone(record.Result)
	return record
}

func deviceFingerprint(kind string, payload json.RawMessage) string {
	h := sha256.New()
	_, _ = h.Write([]byte(kind))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

// DerivedDeviceActionID returns the stable case-local ID used when the native
// phone_write contract has no explicit action ID. Case authors can use the same
// helper to script the corresponding DevicePlan.
func DerivedDeviceActionID(kind string, payload json.RawMessage) (string, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "", fmt.Errorf("%w: device kind is required", ErrInvalidDevicePlan)
	}
	canonical, err := canonicalJSON(payload)
	if err != nil {
		return "", fmt.Errorf("%w: action payload: %w", ErrInvalidDevicePlan, err)
	}
	return "action-" + deviceFingerprint(kind, canonical)[:24], nil
}
