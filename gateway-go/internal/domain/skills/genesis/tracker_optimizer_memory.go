package genesis

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// SkillOptimizerMemoryEntry is the slow/meta-update memory from SkillOpt,
// adapted to Deneb's sparse production loop. Accepted directions and rejected /
// rolled-back directions are persisted outside the skill body so future epochs
// can preserve stable improvements without repeatedly overfitting the document.
type SkillOptimizerMemoryEntry struct {
	SkillName        string   `json:"skillName"`
	AcceptedCount    int      `json:"acceptedCount,omitempty"`
	RejectedCount    int      `json:"rejectedCount,omitempty"`
	RolledBackCount  int      `json:"rolledBackCount,omitempty"`
	LastAcceptedAt   int64    `json:"lastAcceptedAt,omitempty"`
	LastRejectedAt   int64    `json:"lastRejectedAt,omitempty"`
	LastRolledBackAt int64    `json:"lastRolledBackAt,omitempty"`
	StableDirections []string `json:"stableDirections,omitempty"`
	AvoidDirections  []string `json:"avoidDirections,omitempty"`
}

type skillOptimizerMemoryState struct {
	UpdatedAt int64                                `json:"updatedAt"`
	Skills    map[string]SkillOptimizerMemoryEntry `json:"skills,omitempty"`
}

const optimizerMemoryMaxDirections = 5

// OptimizerMemory returns the slow/meta update memory for a skill. Missing
// entries are returned as an empty entry so callers can render a stable shape.
func (t *Tracker) OptimizerMemory(skillName string) (SkillOptimizerMemoryEntry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := strings.TrimSpace(skillName)
	state, err := t.loadOptimizerMemoryLocked()
	if err != nil {
		return SkillOptimizerMemoryEntry{SkillName: key}, err
	}
	if state != nil && state.Skills != nil {
		if entry, ok := state.Skills[key]; ok && optimizerMemoryHasSignal(entry) {
			return entry, nil
		}
	}
	entry, err := t.optimizerMemoryFromLifecycleLocked(key, nil)
	if err != nil {
		return SkillOptimizerMemoryEntry{SkillName: key}, err
	}
	return entry, nil
}

func (t *Tracker) recordOptimizerMemoryLocked(skillName, outcome, note string, at int64) {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" || t.optimizerMemoryPath == "" {
		return
	}
	state, err := t.loadOptimizerMemoryLocked()
	if err != nil {
		if t.logger != nil {
			t.logger.Warn("genesis-tracker: optimizer memory read failed", "skill", skillName, "error", err)
		}
		return
	}
	if state.Skills == nil {
		state.Skills = make(map[string]SkillOptimizerMemoryEntry)
	}
	entry := state.Skills[skillName]
	entry.SkillName = skillName
	if !optimizerMemoryHasSignal(entry) {
		backfilled, err := t.optimizerMemoryFromLifecycleLocked(skillName, &optimizerMemorySkip{
			Outcome:   outcome,
			Note:      note,
			CreatedAt: at,
		})
		if err != nil {
			if t.logger != nil {
				t.logger.Warn("genesis-tracker: optimizer memory lifecycle backfill failed", "skill", skillName, "error", err)
			}
		} else {
			entry = backfilled
		}
	}
	applyOptimizerMemoryOutcome(&entry, outcome, note, at)
	state.Skills[skillName] = entry
	if err := t.saveOptimizerMemoryLocked(state); err != nil && t.logger != nil {
		t.logger.Warn("genesis-tracker: optimizer memory write failed", "skill", skillName, "error", err)
	}
}

type optimizerMemorySkip struct {
	Outcome   string
	Note      string
	CreatedAt int64
}

func (t *Tracker) optimizerMemoryFromLifecycleLocked(skillName string, skip *optimizerMemorySkip) (SkillOptimizerMemoryEntry, error) {
	entry := SkillOptimizerMemoryEntry{SkillName: skillName}
	if strings.TrimSpace(skillName) == "" || t.logPath == "" {
		return entry, nil
	}
	events, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return entry, fmt.Errorf("genesis-tracker: load lifecycle optimizer memory: %w", err)
	}
	skipped := false
	for _, event := range events {
		if event.SkillName != skillName {
			continue
		}
		outcome, note, ok := lifecycleOptimizerOutcome(event)
		if !ok {
			continue
		}
		if skip != nil && !skipped && event.CreatedAt == skip.CreatedAt && outcome == skip.Outcome && note == skip.Note {
			skipped = true
			continue
		}
		applyOptimizerMemoryOutcome(&entry, outcome, note, event.CreatedAt)
	}
	return entry, nil
}

func lifecycleOptimizerOutcome(event LifecycleLogEntry) (string, string, bool) {
	switch event.Type {
	case "evolved":
		return "accepted", event.Description, true
	case "evolve_rejected":
		return "rejected", event.Reason, true
	case "evolve_rolled_back":
		note := strings.TrimSpace(event.Reason)
		if note == "" {
			note = strings.TrimSpace(event.Description)
		}
		if note == "" {
			note = "post-evolve rollback fired"
		}
		return "rolled_back", note, true
	default:
		return "", "", false
	}
}

func optimizerMemoryHasSignal(entry SkillOptimizerMemoryEntry) bool {
	return entry.AcceptedCount > 0 ||
		entry.RejectedCount > 0 ||
		entry.RolledBackCount > 0 ||
		entry.LastAcceptedAt > 0 ||
		entry.LastRejectedAt > 0 ||
		entry.LastRolledBackAt > 0 ||
		len(entry.StableDirections) > 0 ||
		len(entry.AvoidDirections) > 0
}

func applyOptimizerMemoryOutcome(entry *SkillOptimizerMemoryEntry, outcome, note string, at int64) {
	if entry == nil {
		return
	}
	direction := strings.TrimSpace(truncateRunes(note, 400))
	switch outcome {
	case "accepted":
		entry.AcceptedCount++
		entry.LastAcceptedAt = at
		entry.StableDirections = prependOptimizerDirection(entry.StableDirections, direction)
	case "rejected":
		entry.RejectedCount++
		entry.LastRejectedAt = at
		entry.AvoidDirections = prependOptimizerDirection(entry.AvoidDirections, direction)
	case "rolled_back":
		entry.RolledBackCount++
		entry.LastRolledBackAt = at
		entry.AvoidDirections = prependOptimizerDirection(entry.AvoidDirections, direction)
	}
}

func prependOptimizerDirection(items []string, item string) []string {
	item = strings.TrimSpace(item)
	if item == "" {
		return items
	}
	out := []string{item}
	for _, existing := range items {
		if existing == item {
			continue
		}
		out = append(out, existing)
		if len(out) >= optimizerMemoryMaxDirections {
			break
		}
	}
	return out
}

func (t *Tracker) loadOptimizerMemoryLocked() (*skillOptimizerMemoryState, error) {
	state := &skillOptimizerMemoryState{Skills: make(map[string]SkillOptimizerMemoryEntry)}
	if t.optimizerMemoryPath == "" {
		return state, nil
	}
	data, err := os.ReadFile(t.optimizerMemoryPath)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: read optimizer memory: %w", err)
	}
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("genesis-tracker: parse optimizer memory: %w", err)
	}
	if state.Skills == nil {
		state.Skills = make(map[string]SkillOptimizerMemoryEntry)
	}
	return state, nil
}

func (t *Tracker) saveOptimizerMemoryLocked(state *skillOptimizerMemoryState) error {
	if t.optimizerMemoryPath == "" || state == nil {
		return nil
	}
	state.UpdatedAt = time.Now().UnixMilli()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("genesis-tracker: encode optimizer memory: %w", err)
	}
	data = append(data, '\n')
	return atomicfile.WriteFile(t.optimizerMemoryPath, data, &atomicfile.Options{Perm: 0o600, Fsync: true})
}
