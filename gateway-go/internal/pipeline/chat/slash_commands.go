package chat

import (
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

	// Strip any bot-username suffix (e.g., "/reset@MyBot" → "reset").
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
	//
	// Scope: operational commands only. The user-facing conversational
	// commands (/model, /think, /pin, /mode, /mail, /insights, /use-forum,
	// Telegram-era /models) were removed — the native client is the sole
	// surface and covers those through its own UI (model picker via
	// miniapp.models.*) or config defaults (agents.defaults.thinking);
	// unrecognized slash text now flows to the LLM as a normal message.
	switch cmd {
	case "help", "도움말", "?":
		// Discovery: list the builtin slash commands. Response is filled by the
		// dispatcher from slashHelpText() so the listing has a single source.
		return &SlashResult{
			Handled: true,
			Command: "help",
		}
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
	case "rollback", "롤백":
		// Subcommand parsing (list/목록 | diff/비교 | restore/복원) happens in
		// rollback_dispatch.go. Korean alias /롤백 routes to the same handler.
		return &SlashResult{
			Handled:  true,
			Response: "",
			Command:  "rollback",
			Args:     args,
		}
	case "update", "업데이트":
		// Bare /update previews available commits; /update 확인 runs the
		// pull + build + restart. Parsed in update_dispatch.go. Korean alias
		// /업데이트 routes to the same handler.
		return &SlashResult{
			Handled:  true,
			Response: "",
			Command:  "update",
			Args:     args,
		}
	// /restart restarts the gateway immediately (no confirm gate — single-user
	// deployment). Legacy /restart 확인 still works. Parsed in
	// restart_dispatch.go. Korean alias /재시작 routes to the same handler.
	case "restart", "재시작":
		return &SlashResult{
			Handled:  true,
			Response: "",
			Command:  "restart",
			Args:     args,
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
