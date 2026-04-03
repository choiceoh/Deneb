// eligibility.go evaluates whether a skill should be included based on
// binary requirements, environment variables, and configuration.
//
// This ports src/agents/skills/config.ts:shouldIncludeSkill() and
// src/shared/config-eval.ts:evaluateRuntimeEligibility() to Go.
package skills

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// SkillConfig represents per-skill configuration from config.skills.entries[key].
type SkillConfig struct {
	Enabled *bool             `json:"enabled,omitempty"`
	APIKey  string            `json:"apiKey,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// EligibilityContext holds all external state needed for eligibility evaluation.
type EligibilityContext struct {
	EnvVars      map[string]string // relevant environment variables snapshot
	SkillConfigs map[string]SkillConfig
	AllowBundled []string // config.skills.allowBundled
	ConfigValues map[string]bool
}

// DefaultEligibilityContext creates a context using the current runtime environment.
func DefaultEligibilityContext() EligibilityContext {
	return EligibilityContext{
		EnvVars:      envSnapshot(),
		SkillConfigs: make(map[string]SkillConfig),
		ConfigValues: make(map[string]bool),
	}
}

func envSnapshot() map[string]string {
	env := make(map[string]string)
	for _, e := range os.Environ() {
		if k, v, ok := strings.Cut(e, "="); ok {
			env[k] = v
		}
	}
	return env
}

// ShouldIncludeSkill evaluates whether a skill entry should be included.
func ShouldIncludeSkill(entry SkillEntry, ctx EligibilityContext) bool {
	skillKey := resolveSkillKeyFromEntry(entry)
	skillCfg := ctx.SkillConfigs[skillKey]

	// Explicitly disabled.
	if skillCfg.Enabled != nil && !*skillCfg.Enabled {
		return false
	}

	// Bundled allowlist check.
	if !isBundledSkillAllowed(entry, ctx.AllowBundled) {
		return false
	}

	return evaluateRuntimeEligibility(entry, skillCfg, ctx)
}

func resolveSkillKeyFromEntry(entry SkillEntry) string {
	if entry.Metadata != nil && entry.Metadata.SkillKey != "" {
		return entry.Metadata.SkillKey
	}
	return entry.Skill.Name
}

func isBundledSkillAllowed(entry SkillEntry, allowBundled []string) bool {
	if len(allowBundled) == 0 {
		return true
	}
	if entry.Skill.Source != SourceBundled {
		return true
	}
	key := resolveSkillKeyFromEntry(entry)
	for _, allowed := range allowBundled {
		if allowed == key || allowed == entry.Skill.Name {
			return true
		}
	}
	return false
}

func evaluateRuntimeEligibility(entry SkillEntry, skillCfg SkillConfig, ctx EligibilityContext) bool {
	meta := entry.Metadata

	// Always flag bypasses requirements.
	if meta != nil && meta.Always {
		return true
	}

	if meta == nil || meta.Requires == nil {
		return true
	}
	requires := meta.Requires

	// Required binaries: all must exist.
	for _, bin := range requires.Bins {
		if !hasBinary(bin) {
			return false
		}
	}

	// Any-of binaries: at least one must exist.
	if len(requires.AnyBins) > 0 {
		found := false
		for _, bin := range requires.AnyBins {
			if hasBinary(bin) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Environment variables.
	for _, envName := range requires.Env {
		if !hasEnvVar(envName, entry, skillCfg, ctx) {
			return false
		}
	}

	// Config paths.
	for _, configPath := range requires.Config {
		if !ctx.ConfigValues[configPath] {
			return false
		}
	}

	return true
}

func hasEnvVar(envName string, entry SkillEntry, skillCfg SkillConfig, ctx EligibilityContext) bool {
	// Check process env.
	if v, ok := ctx.EnvVars[envName]; ok && v != "" {
		return true
	}
	// Check skill config env.
	if v, ok := skillCfg.Env[envName]; ok && v != "" {
		return true
	}
	// Check apiKey → primaryEnv mapping.
	if skillCfg.APIKey != "" && entry.Metadata != nil && entry.Metadata.PrimaryEnv == envName {
		return true
	}
	return false
}

// hasBinary checks if a binary is available in PATH.
// Results are cached per binary name.
var (
	hasBinaryCache   = make(map[string]bool)
	hasBinaryCacheMu sync.RWMutex
)

func hasBinary(bin string) bool {
	hasBinaryCacheMu.RLock()
	if v, ok := hasBinaryCache[bin]; ok {
		hasBinaryCacheMu.RUnlock()
		return v
	}
	hasBinaryCacheMu.RUnlock()

	_, err := exec.LookPath(bin)
	found := err == nil

	// Also check PATH manually for better cross-platform support.
	if !found {
		found = lookPathManual(bin)
	}

	hasBinaryCacheMu.Lock()
	hasBinaryCache[bin] = found
	hasBinaryCacheMu.Unlock()
	return found
}

func lookPathManual(bin string) bool {
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return false
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, bin)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}

// FilterEligibleSkills filters a slice of entries by eligibility.
func FilterEligibleSkills(entries []SkillEntry, ctx EligibilityContext) []SkillEntry {
	var result []SkillEntry
	for _, entry := range entries {
		if ShouldIncludeSkill(entry, ctx) {
			result = append(result, entry)
		}
	}
	return result
}

// FilterBySkillFilter applies a name-based skill filter on top of eligible entries.
// If filter is nil, all entries pass. If filter is empty, no entries pass.
func FilterBySkillFilter(entries []SkillEntry, filter []string) []SkillEntry {
	if filter == nil {
		return entries
	}
	normalized := NormalizeSkillFilter(filter)
	if len(normalized) == 0 {
		return nil
	}
	filterSet := make(map[string]bool, len(normalized))
	for _, f := range normalized {
		filterSet[strings.ToLower(f)] = true
	}
	var result []SkillEntry
	for _, entry := range entries {
		key := strings.ToLower(resolveSkillKeyFromEntry(entry))
		if filterSet[key] {
			result = append(result, entry)
		}
	}
	return result
}
