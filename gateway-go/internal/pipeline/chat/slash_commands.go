package chat

import (
	"fmt"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

// SlashResult holds the result of parsing a slash command from user input.
type SlashResult struct {
	Handled   bool             // If true, the message was a slash command and should not be sent to LLM.
	Response  string           // Direct response to send back to the user.
	Command   string           // The parsed command name (e.g., "reset", "model").
	Args      string           // Arguments after the command.
	SkillType skills.SkillType // The type of skill that handled this command (empty if builtin).
	SkillName string           // The skill name if dispatched via skill routing.
}

// ParseSlashCommand checks if a message starts with a slash command.
// First tries skill-based routing (local/system types), then falls back to
// hardcoded builtins. Returns nil if not a recognized command.
func ParseSlashCommand(text string) *SlashResult {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return nil
	}

	// Extract command and args.
	parts := strings.SplitN(trimmed[1:], " ", 2)
	cmd := strings.ToLower(parts[0])

	// Strip Telegram bot username suffix (e.g., "/reset@MyBot" → "reset").
	if idx := strings.IndexByte(cmd, '@'); idx != -1 {
		cmd = cmd[:idx]
	}
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	// Try skill-based routing for local and system skills.
	if result := trySkillCommand(cmd, args); result != nil {
		return result
	}

	// Fall back to hardcoded builtins.
	switch cmd {
	case "reset":
		return &SlashResult{
			Handled:  true,
			Response: "세션이 초기화되었습니다.",
			Command:  "reset",
		}
	case "status":
		return &SlashResult{
			Handled:  true,
			Response: "", // Will be filled by the handler with actual status.
			Command:  "status",
		}
	case "kill", "stop", "cancel":
		return &SlashResult{
			Handled:  true,
			Response: "실행이 중단되었습니다.",
			Command:  "kill",
		}
	case "model":
		// Accepts model ID ("google/gemini-3.1-pro") or role name ("main", "lightweight", "fallback").
		if args == "" {
			return &SlashResult{
				Handled:  true,
				Response: "사용법: /model <model-name 또는 역할명(main|lightweight|pilot|fallback)>",
				Command:  "model",
			}
		}
		return &SlashResult{
			Handled:  true,
			Response: fmt.Sprintf("모델이 %q(으)로 변경되었습니다.", args),
			Command:  "model",
			Args:     args,
		}
	case "models":
		return &SlashResult{
			Handled:  true,
			Response: "모델 퀵체인지는 텔레그램에서만 지원됩니다.",
			Command:  "models",
		}
	case "think":
		return &SlashResult{
			Handled:  true,
			Response: "사고 모드가 토글되었습니다.",
			Command:  "think",
		}
	case "coordinator", "코디네이터":
		return &SlashResult{
			Handled:  true,
			Response: "코디네이터 모드가 활성화되었습니다. 워커 에이전트를 조율하여 작업을 수행합니다.",
			Command:  "coordinator",
		}
	case "mode", "모드":
		return &SlashResult{
			Handled:  true,
			Response: "",
			Command:  "mode",
			Args:     args,
		}
	case "mail", "메일":
		return &SlashResult{
			Handled:  true,
			Response: "",
			Command:  "mail",
		}
	default:
		// Not a recognized slash command; pass through to LLM.
		return nil
	}
}

// trySkillCommand checks the cached skills snapshot for local/system skill matches.
func trySkillCommand(cmd, args string) *SlashResult {
	snapshot := CachedSkillsSnapshot()
	if snapshot == nil {
		return nil
	}

	// Match command name against resolved skills.
	for _, ps := range snapshot.ResolvedSkills {
		skillCmd := strings.ToLower(ps.Name)
		if skillCmd != cmd {
			continue
		}

		// Look up the full entry to get type info.
		entry := findSkillEntryByName(snapshot, ps.Name)
		if entry == nil {
			continue
		}

		switch entry.Skill.Type {
		case skills.SkillTypeLocal:
			output, err := skills.ExecuteLocalSkill(*entry, args)
			if err != nil {
				return &SlashResult{
					Handled:   true,
					Response:  skills.WrapSkillError(ps.Name, string(skills.SkillTypeLocal), args, err.Error()),
					Command:   cmd,
					Args:      args,
					SkillType: skills.SkillTypeLocal,
					SkillName: ps.Name,
				}
			}
			return &SlashResult{
				Handled:   true,
				Response:  skills.WrapSkillInvocation(ps.Name, string(skills.SkillTypeLocal), args, output),
				Command:   cmd,
				Args:      args,
				SkillType: skills.SkillTypeLocal,
				SkillName: ps.Name,
			}

		case skills.SkillTypeSystem:
			output, err := skills.ExecuteSystemSkill(*entry, args)
			if err != nil {
				return &SlashResult{
					Handled:   true,
					Response:  skills.WrapSkillError(ps.Name, string(skills.SkillTypeSystem), args, err.Error()),
					Command:   cmd,
					Args:      args,
					SkillType: skills.SkillTypeSystem,
					SkillName: ps.Name,
				}
			}
			return &SlashResult{
				Handled:   true,
				Response:  skills.WrapSkillInvocation(ps.Name, string(skills.SkillTypeSystem), args, output),
				Command:   cmd,
				Args:      args,
				SkillType: skills.SkillTypeSystem,
				SkillName: ps.Name,
			}

		default:
			// prompt-type skills are handled by the LLM, not here.
			return nil
		}
	}
	return nil
}

// findSkillEntryByName finds a skill entry from the snapshot by matching the full
// discovery data. Since FullSkillSnapshot only stores PromptSkill (lightweight),
// we re-discover via the cached snapshot's metadata.
func findSkillEntryByName(_ *skills.FullSkillSnapshot, name string) *skills.SkillEntry {
	// The snapshot stores resolved skills but not full entries with Type.
	// We need to look up from the cached discovery. Use a simple re-discovery
	// approach keyed by the snapshot version.
	entries := getCachedSkillEntries()
	for i := range entries {
		if strings.EqualFold(entries[i].Skill.Name, name) {
			return &entries[i]
		}
	}
	return nil
}

// cachedSkillEntries stores the last set of discovered skill entries.
var cachedSkillEntries struct {
	mu      sync.RWMutex
	entries []skills.SkillEntry
	version int64
}

// SetCachedSkillEntries updates the cached skill entries (called during snapshot rebuild).
func SetCachedSkillEntries(entries []skills.SkillEntry, version int64) {
	cachedSkillEntries.mu.Lock()
	cachedSkillEntries.entries = entries
	cachedSkillEntries.version = version
	cachedSkillEntries.mu.Unlock()
}

func getCachedSkillEntries() []skills.SkillEntry {
	cachedSkillEntries.mu.RLock()
	defer cachedSkillEntries.mu.RUnlock()
	return cachedSkillEntries.entries
}
