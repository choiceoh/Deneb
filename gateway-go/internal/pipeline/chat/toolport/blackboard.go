// Package toolport — typed blackboard for multi-tool workflow I/O contracts.
//
// A Blackboard is a run-scoped, fail-closed store of named JSON values. Agents
// declare step contracts (required inputs / promised outputs) and pass only
// those keys between tools instead of free-text intermediate summaries.
package toolport

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
)

const (
	maxBlackboardKeys     = 64
	maxBlackboardKeyRunes = 64
	maxBlackboardValueLen = 16 * 1024
	maxBlackboardSteps    = 24
)

// StepContract is one subtask in a blackboard plan: a goal plus typed I/O keys.
type StepContract struct {
	ID      string   `json:"id"`
	Goal    string   `json:"goal,omitempty"`
	Inputs  []string `json:"inputs,omitempty"`
	Outputs []string `json:"outputs,omitempty"`
}

// BoardValue is one typed entry on the blackboard.
type BoardValue struct {
	Key    string          `json:"key"`
	Value  json.RawMessage `json:"value"`
	Source string          `json:"source,omitempty"`
}

// Blackboard is a thread-safe run-scoped typed store with optional step plans.
// All exported methods take b.mu; helpers with a Locked suffix assume the caller
// already holds it (no re-entrant lock acquisition).
type Blackboard struct {
	mu     sync.Mutex
	values map[string]BoardValue
	steps  []StepContract
	active string // current step id; empty when none
}

// NewBlackboard returns an empty blackboard.
func NewBlackboard() *Blackboard {
	return &Blackboard{values: make(map[string]BoardValue, 8)}
}

// Put stores a JSON value under key. Empty keys, oversize payloads, and invalid
// JSON are rejected. Existing keys are overwritten.
func (b *Blackboard) Put(key string, value json.RawMessage, source string) error {
	if b == nil {
		return fmt.Errorf("blackboard: not available")
	}
	key, err := normalizeBoardKey(key)
	if err != nil {
		return err
	}
	raw, err := normalizeBoardValue(value)
	if err != nil {
		return fmt.Errorf("blackboard: key %q: %w", key, err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.putLocked(key, raw, source)
}

// Get returns a value by key.
func (b *Blackboard) Get(key string) (BoardValue, bool) {
	if b == nil {
		return BoardValue{}, false
	}
	key, err := normalizeBoardKey(key)
	if err != nil {
		return BoardValue{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.values[key]
	if !ok {
		return BoardValue{}, false
	}
	return BoardValue{
		Key:    v.Key,
		Value:  append(json.RawMessage(nil), v.Value...),
		Source: v.Source,
	}, true
}

// Require returns every requested key or fails closed listing the missing ones.
func (b *Blackboard) Require(keys []string) (map[string]json.RawMessage, error) {
	if b == nil {
		return nil, fmt.Errorf("blackboard: not available")
	}
	normalized, err := normalizeKeyList(keys)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.requireLocked(normalized)
}

// Plan installs an ordered multi-step I/O contract. Replaces any prior plan and
// clears the active step; existing values are retained.
func (b *Blackboard) Plan(steps []StepContract) error {
	if b == nil {
		return fmt.Errorf("blackboard: not available")
	}
	if len(steps) == 0 {
		return fmt.Errorf("blackboard: plan requires at least one step")
	}
	if len(steps) > maxBlackboardSteps {
		return fmt.Errorf("blackboard: at most %d steps", maxBlackboardSteps)
	}
	normalized := make([]StepContract, 0, len(steps))
	seen := make(map[string]struct{}, len(steps))
	for i, step := range steps {
		id, err := normalizeBoardKey(step.ID)
		if err != nil {
			return fmt.Errorf("blackboard: step[%d].id: %w", i, err)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("blackboard: duplicate step id %q", id)
		}
		seen[id] = struct{}{}
		inputs, err := normalizeKeyList(step.Inputs)
		if err != nil {
			return fmt.Errorf("blackboard: step %q inputs: %w", id, err)
		}
		outputs, err := normalizeKeyList(step.Outputs)
		if err != nil {
			return fmt.Errorf("blackboard: step %q outputs: %w", id, err)
		}
		if len(inputs) == 0 && len(outputs) == 0 {
			return fmt.Errorf("blackboard: step %q needs inputs or outputs", id)
		}
		normalized = append(normalized, StepContract{
			ID:      id,
			Goal:    strings.TrimSpace(step.Goal),
			Inputs:  inputs,
			Outputs: outputs,
		})
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.steps = normalized
	b.active = ""
	return nil
}

// BeginStep activates a planned step after verifying its declared inputs.
// Returns the input values for the step.
func (b *Blackboard) BeginStep(id string) (map[string]json.RawMessage, error) {
	if b == nil {
		return nil, fmt.Errorf("blackboard: not available")
	}
	id, err := normalizeBoardKey(id)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	step, ok := b.findStepLocked(id)
	if !ok {
		return nil, fmt.Errorf("blackboard: unknown step %q", id)
	}
	values, err := b.requireLocked(step.Inputs)
	if err != nil {
		return nil, fmt.Errorf("blackboard: begin step %q: %w", id, err)
	}
	b.active = id
	return values, nil
}

// EndStep writes the step's declared outputs and clears the active step.
// Missing declared outputs fail closed; undeclared keys are rejected.
func (b *Blackboard) EndStep(id string, outputs map[string]json.RawMessage) error {
	if b == nil {
		return fmt.Errorf("blackboard: not available")
	}
	id, err := normalizeBoardKey(id)
	if err != nil {
		return err
	}

	prepared := make(map[string]json.RawMessage, len(outputs))
	for rawKey, rawVal := range outputs {
		key, kerr := normalizeBoardKey(rawKey)
		if kerr != nil {
			return fmt.Errorf("blackboard: end step %q: %w", id, kerr)
		}
		val, verr := normalizeBoardValue(rawVal)
		if verr != nil {
			return fmt.Errorf("blackboard: end step %q output %q: %w", id, key, verr)
		}
		prepared[key] = val
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	step, ok := b.findStepLocked(id)
	if !ok {
		return fmt.Errorf("blackboard: unknown step %q", id)
	}
	if b.active != "" && b.active != id {
		return fmt.Errorf("blackboard: step %q is active; cannot end %q", b.active, id)
	}
	declared := make(map[string]struct{}, len(step.Outputs))
	for _, key := range step.Outputs {
		declared[key] = struct{}{}
	}
	for key := range prepared {
		if _, ok := declared[key]; !ok {
			return fmt.Errorf("blackboard: end step %q: undeclared output %q", id, key)
		}
	}
	missing := make([]string, 0)
	for _, key := range step.Outputs {
		raw, present := prepared[key]
		if !present || string(raw) == "null" {
			missing = append(missing, key)
			continue
		}
		if err := b.putLocked(key, raw, "step:"+id); err != nil {
			return fmt.Errorf("blackboard: end step %q output %q: %w", id, key, err)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("blackboard: end step %q missing outputs: %s", id, strings.Join(missing, ", "))
	}
	if b.active == id {
		b.active = ""
	}
	return nil
}

// List returns a stable snapshot of stored values.
func (b *Blackboard) List() []BoardValue {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]BoardValue, 0, len(b.values))
	for _, v := range b.values {
		out = append(out, BoardValue{
			Key:    v.Key,
			Value:  append(json.RawMessage(nil), v.Value...),
			Source: v.Source,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Steps returns the current plan.
func (b *Blackboard) Steps() []StepContract {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]StepContract, len(b.steps))
	copy(out, b.steps)
	for i := range out {
		out[i].Inputs = append([]string(nil), out[i].Inputs...)
		out[i].Outputs = append([]string(nil), out[i].Outputs...)
	}
	return out
}

// ActiveStep returns the id of the active step, if any.
func (b *Blackboard) ActiveStep() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.active
}

// Clear removes all values and the plan.
func (b *Blackboard) Clear() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.values = make(map[string]BoardValue, 8)
	b.steps = nil
	b.active = ""
}

func (b *Blackboard) findStepLocked(id string) (StepContract, bool) {
	for _, step := range b.steps {
		if step.ID == id {
			return step, true
		}
	}
	return StepContract{}, false
}

func (b *Blackboard) putLocked(key string, raw json.RawMessage, source string) error {
	if _, exists := b.values[key]; !exists && len(b.values) >= maxBlackboardKeys {
		return fmt.Errorf("blackboard: at most %d keys", maxBlackboardKeys)
	}
	b.values[key] = BoardValue{Key: key, Value: raw, Source: strings.TrimSpace(source)}
	return nil
}

func (b *Blackboard) requireLocked(keys []string) (map[string]json.RawMessage, error) {
	out := make(map[string]json.RawMessage, len(keys))
	missing := make([]string, 0)
	for _, key := range keys {
		v, ok := b.values[key]
		if !ok {
			missing = append(missing, key)
			continue
		}
		out[key] = append(json.RawMessage(nil), v.Value...)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("blackboard: missing required keys: %s", strings.Join(missing, ", "))
	}
	return out, nil
}

func normalizeBoardKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("empty key")
	}
	if len([]rune(key)) > maxBlackboardKeyRunes {
		return "", fmt.Errorf("key longer than %d runes", maxBlackboardKeyRunes)
	}
	for i, r := range key {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return "", fmt.Errorf("key %q must start with a letter or _", key)
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return "", fmt.Errorf("key %q may contain only letters, digits, and _", key)
		}
	}
	return key, nil
}

func normalizeKeyList(keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, raw := range keys {
		key, err := normalizeBoardKey(raw)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out, nil
}

func normalizeBoardValue(value json.RawMessage) (json.RawMessage, error) {
	if len(value) == 0 {
		return nil, fmt.Errorf("empty value")
	}
	if len(value) > maxBlackboardValueLen {
		return nil, fmt.Errorf("value longer than %d bytes", maxBlackboardValueLen)
	}
	if !json.Valid(value) {
		return nil, fmt.Errorf("value is not valid JSON")
	}
	var compact json.RawMessage
	if err := json.Unmarshal(value, &compact); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(compact)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
