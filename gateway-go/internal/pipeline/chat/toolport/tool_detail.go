package toolport

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// ToolStreamDetail extracts a short human hint from a tool call's raw JSON
// input for the native client's waiting chip — the difference between
// "메일 확인 중" and "메일 확인 중: 아르고에너지". Only a curated key per tool
// is surfaced (queries, commands, file names — never message bodies or file
// contents), and the result is whitespace-collapsed and rune-truncated so it
// stays a chip-sized label. Returns "" when the tool has no curated keys or
// none of them carry a usable string.
func ToolStreamDetail(name string, input []byte) string {
	keys, ok := toolDetailKeys[name]
	if !ok || len(input) == 0 {
		return ""
	}
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return ""
	}
	for _, key := range keys {
		raw, ok := args[key].(string)
		if !ok {
			continue
		}
		val := strings.Join(strings.Fields(raw), " ")
		if val == "" {
			continue
		}
		if strings.HasSuffix(key, "path") {
			val = filepath.Base(val)
		}
		return textutil.TruncateRunes(val, maxToolDetailRunes, "…")
	}
	return ""
}

// maxToolDetailRunes caps the chip hint length. Rune-based so Korean text
// truncates cleanly.
const maxToolDetailRunes = 48

// toolDetailKeys maps a tool name to the input keys worth surfacing, in
// preference order. Keys ending in "path" are reduced to their base name.
// Deliberately omitted: message/send_file (bodies), sessions/process/observe
// (operator plumbing), heartbeat_update, polaris, skills, watch, cron
// (no single arg reads as a useful hint).
var toolDetailKeys = map[string][]string{
	"calendar":     {"summary"},
	"people":       {"query"},
	"edit":         {"file_path"},
	"exec":         {"command"},
	"files":        {"query", "path"},
	"graphify":     {"question", "node"},
	"grep":         {"pattern"},
	"knowledge":    {"query", "title"},
	"mail_archive": {"query", "message_id"},
	"read":         {"file_path"},
	"web":          {"query", "url"},
	"wiki":         {"query", "title"},
	"write":        {"file_path"},
}

// truncateDetail caps s to maxRunes runes, appending an ellipsis when cut.

// DetailedToolNames returns every tool name the detail-key map covers. See
// LabelledToolNames for why this is exported.
func DetailedToolNames() []string {
	out := make([]string, 0, len(toolDetailKeys))
	for name := range toolDetailKeys {
		out = append(out, name)
	}
	return out
}
