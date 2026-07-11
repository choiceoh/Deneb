package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// Replay-input reconstruction split out of skill_lifecycle_tool.go (pure
// move, no behavior change): transcript mining, fragment extraction/redaction,
// and session-context building for validation replays.

func skillReplayInputFromTranscript(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "user: ") {
			return truncateRunes(strings.TrimSpace(strings.TrimPrefix(line, "user: ")), 500)
		}
	}
	return truncateRunes(strings.TrimSpace(text), 500)
}

func skillReplayContextFromTranscript(text string) []string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, 4)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "user: ") || strings.HasPrefix(line, "assistant: ") {
			out = append(out, truncateRunes(line, 300))
		}
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func skillValidationCaseIDFromSession(sessionKey string) string {
	var b strings.Builder
	b.WriteString("session-")
	for _, r := range strings.ToLower(strings.TrimSpace(sessionKey)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ':':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
		if b.Len() >= 100 {
			break
		}
	}
	if b.String() == "session-" {
		return "session-replay"
	}
	return b.String()
}

func skillReplayInputIncludes(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	var decoded any
	var out []string
	if err := json.Unmarshal([]byte(input), &decoded); err == nil {
		collectReplayInputFragments(decoded, "", &out)
	}
	if len(out) == 0 && len([]rune(input)) <= 160 && !trivialReplayInput(input) {
		out = append(out, input)
	}
	return appendUniqueStrings(nil, out...)
}

func trivialReplayInput(input string) bool {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "", "{}", "[]", "null":
		return true
	default:
		return false
	}
}

func collectReplayInputFragments(value any, key string, out *[]string) {
	if len(*out) >= 3 || replaySecretKey(key) {
		return
	}
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := v[k]
			collectReplayInputFragments(child, k, out)
			if len(*out) >= 3 {
				return
			}
		}
	case []any:
		for _, child := range v {
			collectReplayInputFragments(child, key, out)
			if len(*out) >= 3 {
				return
			}
		}
	case string:
		if replayInterestingKey(key) {
			appendReplayInputFragment(out, key, v)
		}
	case float64, bool:
		if replayInterestingKey(key) {
			appendReplayInputFragment(out, key, fmt.Sprint(v))
		}
	}
}

func replayInterestingKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "action", "cmd", "command", "path", "query", "q", "url", "sessionkey", "skillname", "ref_id", "id", "filename":
		return true
	default:
		return false
	}
}

func replaySecretKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, marker := range []string{"token", "secret", "password", "apikey", "api_key", "authorization", "cookie", "initdata"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func appendReplayInputFragment(out *[]string, key, value string) {
	for _, fragment := range replayInputFragmentsForKey(key, value) {
		fragment = strings.TrimSpace(fragment)
		if fragment == "" || looksOpaqueReplayFragment(fragment) {
			continue
		}
		*out = append(*out, truncateRunes(fragment, 180))
		if len(*out) >= 3 {
			return
		}
	}
}

func replayInputFragmentsForKey(key, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "cmd", "command":
		return replayCommandIntentFragments(value)
	default:
		return []string{value}
	}
}

func replayCommandIntentFragments(command string) []string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil
	}
	var out []string
	appendUnique := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range out {
			if strings.EqualFold(existing, value) {
				return
			}
		}
		out = append(out, value)
	}
	for i := 0; i < len(fields); i++ {
		token := cleanReplayCommandToken(fields[i])
		switch token {
		case "ssh":
			appendUnique(replaySSHIntent("ssh", fields, i+1))
		case "tailscale":
			if i+1 < len(fields) && cleanReplayCommandToken(fields[i+1]) == "ssh" {
				appendUnique(replaySSHIntent("tailscale ssh", fields, i+2))
			}
		case "systemctl":
			appendUnique(systemctlReplayIntent(fields[i:]))
		}
		if len(out) >= 3 {
			return out
		}
	}
	if len(out) == 0 {
		appendUnique(genericCommandIntent(fields))
	}
	return out
}

func replaySSHIntent(prefix string, fields []string, start int) string {
	for i := start; i < len(fields); i++ {
		token := cleanReplayCommandToken(fields[i])
		if token == "" {
			continue
		}
		if strings.HasPrefix(token, "-") {
			if !strings.Contains(token, "=") && i+1 < len(fields) {
				next := cleanReplayCommandToken(fields[i+1])
				if next != "" && !strings.HasPrefix(next, "-") {
					i++
				}
			}
			continue
		}
		return prefix + " " + token
	}
	return ""
}

func cleanReplayCommandToken(token string) string {
	return strings.Trim(strings.TrimSpace(token), `"'`)
}

func systemctlReplayIntent(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	parts := []string{cleanReplayCommandToken(fields[0])}
	hasUser := false
	subcommand := ""
	for _, raw := range fields[1:] {
		token := cleanReplayCommandToken(raw)
		if token == "" {
			continue
		}
		if token == "--user" {
			hasUser = true
			continue
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		if subcommand == "" {
			subcommand = token
			break
		}
	}
	if hasUser {
		parts = append(parts, "--user")
	}
	if subcommand != "" {
		parts = append(parts, subcommand)
	}
	return strings.Join(parts, " ")
}

func genericCommandIntent(fields []string) string {
	command := cleanReplayCommandToken(fields[0])
	if command == "" {
		return ""
	}
	for _, raw := range fields[1:] {
		token := cleanReplayCommandToken(raw)
		if token == "" || strings.HasPrefix(token, "-") {
			continue
		}
		return command + " " + token
	}
	return command
}

func looksOpaqueReplayFragment(value string) bool {
	if len([]rune(value)) < 48 {
		return false
	}
	if strings.ContainsAny(value, " /:-_.") {
		return false
	}
	return true
}

func buildSkillLifecycleSessionContext(store toolctx.TranscriptStore, sessionKey string) (genesis.SessionContext, error) {
	sctx := genesis.SessionContext{Key: sessionKey}
	if store == nil {
		return sctx, nil
	}

	msgs, _, err := store.Load(sessionKey, 200)
	if err != nil {
		return sctx, err
	}

	var textParts []string
	pendingToolResults := make(map[string]int)
	for _, msg := range msgs {
		if msg.Role == "assistant" {
			sctx.Turns++
		}
		if text := msg.TextContent(); text != "" {
			textParts = append(textParts, msg.Role+": "+text)
		}
		extractSkillLifecycleToolActivities(msg.Content, pendingToolResults, &sctx.ToolActivities)
	}
	sctx.AllText = strings.Join(textParts, "\n")
	return sctx, nil
}

func extractSkillLifecycleToolActivities(content json.RawMessage, pending map[string]int, activities *[]genesis.ToolActivity) {
	if len(content) == 0 || content[0] != '[' {
		return
	}
	var blocks []llm.ContentBlock
	if json.Unmarshal(content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		switch b.Type {
		case "tool_use":
			if strings.TrimSpace(b.Name) == "" {
				continue
			}
			*activities = append(*activities, genesis.ToolActivity{
				Name:  b.Name,
				Input: compactJSONForReplay(b.Input),
			})
			if b.ID != "" && pending != nil {
				pending[b.ID] = len(*activities) - 1
			}
		case "tool_result":
			if pending == nil || b.ToolUseID == "" {
				continue
			}
			idx, ok := pending[b.ToolUseID]
			if !ok || idx < 0 || idx >= len(*activities) {
				continue
			}
			(*activities)[idx].IsError = b.IsError
			(*activities)[idx].Output = truncateRunes(strings.TrimSpace(b.Content), 2000)
		}
	}
}

func compactJSONForReplay(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err == nil {
		return truncateRunes(buf.String(), 1000)
	}
	return truncateRunes(strings.TrimSpace(string(raw)), 1000)
}
