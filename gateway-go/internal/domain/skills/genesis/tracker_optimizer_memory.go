package genesis

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
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
	if state == nil || state.Skills == nil {
		return SkillOptimizerMemoryEntry{SkillName: key}, nil
	}
	if entry, ok := state.Skills[key]; ok {
		return entry, nil
	}
	return SkillOptimizerMemoryEntry{SkillName: key}, nil
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
	state.Skills[skillName] = entry
	if err := t.saveOptimizerMemoryLocked(state); err != nil && t.logger != nil {
		t.logger.Warn("genesis-tracker: optimizer memory write failed", "skill", skillName, "error", err)
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
