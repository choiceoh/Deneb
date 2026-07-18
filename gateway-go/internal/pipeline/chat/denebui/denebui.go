// Package denebui defines the schema for Deneb-style interactive UI blocks that the
// agent emits inside a ```deneb-ui fenced code block in its reply text. The native
// Android client (vendored from SimonSchubert/Kai, Apache-2.0) renders these as
// interactive Compose screens; button presses round-trip back as new turns.
//
// The block travels as a fenced JSON object in normal assistant text — there is
// no separate wire event — so this package only needs to (a) extract the fence
// and (b) structurally validate the JSON against Kai's KaiUiNode schema. The
// client's own parser does the lenient repair + actual rendering, so validation
// here is a server-side quality gate, not a full reimplementation.
//
// Schema source: SimonSchubert/Kai ui/dynamicui/KaiUiNode.kt + UiAction.kt.
// The polymorphic discriminator key is "type" (kotlinx default class
// discriminator); each node carries a "type" string from nodeSpecs below.
package denebui

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FenceInfo is the markdown fence info-string that marks a deneb-ui block.
// Matched case-insensitively, mirroring Kai's BlockScanner.kt.
const FenceInfo = "deneb-ui"

// Issue describes a single schema violation at a JSON-ish path.
type Issue struct {
	Path string
	Msg  string
}

// String returns the human-readable representation.
func (is Issue) String() string { return is.Path + ": " + is.Msg }

// Recoverable reports whether the issue is content-preserving: the HTML parser
// already unwrapped the offending unknown tag (children hoisted, or a void tag
// with no content), so the parsed tree still renders faithfully on all three
// clients — their parsers unwrap unknown tags the same way. NormalizeFinalReply
// delivers a card whose issues are ALL recoverable instead of downgrading it to
// plain text; the issue stays logged as drift telemetry. Structural violations
// (missing interactive id, invalid action, unparseable body) are not recoverable.
func (is Issue) Recoverable() bool { return strings.HasPrefix(is.Msg, "unknown tag <") }

// nodeSpec captures the structural rules for one node type.
type nodeSpec struct {
	requireID    bool                // true if "id" must be a non-empty string
	childFields  []string            // fields holding arrays of child nodes
	actionFields []string            // fields holding a UiAction object
	enums        map[string][]string // field -> allowed string values
}

var (
	textStyles      = []string{"headline", "title", "body", "caption"}
	keyboardTypes   = []string{"text", "number", "decimal", "email", "phone", "url"}
	buttonVariants  = []string{"filled", "outlined", "text", "tonal"}
	alertSeverities = []string{"info", "success", "warning", "error"}
	chartTypes      = []string{"bar", "line"}
	actionTypes     = []string{"callback", "toggle", "open_url", "copy_to_clipboard"}
)

// nodeSpecs is the registry of every known DenebUiNode type. A type absent here is
// reported as unknown. Fields not modeled (plain strings/ints/bools) are ignored
// — unknown extra fields are tolerated, matching the client's lenient parser.
var nodeSpecs = map[string]nodeSpec{
	// Layout containers (children are nodes).
	"column":    {childFields: []string{"children"}},
	"row":       {childFields: []string{"children"}},
	"card":      {childFields: []string{"children"}},
	"box":       {childFields: []string{"children"}},
	"accordion": {childFields: []string{"children"}},
	"list":      {childFields: []string{"items"}},
	"divider":   {},
	// "tabs" is handled specially (tabs[].children) in validateObject.
	"tabs": {},
	// Content.
	"text":     {enums: map[string][]string{"style": textStyles}},
	"markdown": {}, // rich-text body (full markdown), e.g. a collapsed report
	"image":    {},
	"icon":     {},
	"code":     {},
	"quote":    {},
	"badge":    {},
	"stat":     {},
	"avatar":   {},
	"table":    {},                                                    // headers/rows are strings, not nodes
	"chart":    {enums: map[string][]string{"chartType": chartTypes}}, // labels/values are parallel arrays
	// Interactive (id-bearing).
	"button":      {actionFields: []string{"action"}, enums: map[string][]string{"variant": buttonVariants}},
	"text_input":  {requireID: true, enums: map[string][]string{"keyboard": keyboardTypes}},
	"date_input":  {requireID: true},
	"time_input":  {requireID: true},
	"checkbox":    {requireID: true},
	"select":      {requireID: true},
	"switch":      {requireID: true},
	"slider":      {requireID: true},
	"radio_group": {requireID: true},
	"chip_group":  {requireID: true}, // chips are {label,value}, not nodes
	// Feedback.
	"progress":  {},
	"alert":     {enums: map[string][]string{"severity": alertSeverities}},
	"countdown": {actionFields: []string{"action"}},
}

// ExtractFences returns the raw bodies of every ```deneb-ui fenced block in text,
// in document order. Fence info match is case-insensitive.
//
// Leniency (mirrors TS splitDenebUi / Kotlin BlockScanner): the opener may be
// glued to a prose tail and may carry the first body tag on the same line
// ("…할게요.```deneb-ui<column>…"), and inside an HTML body a ``` run glued to
// the last body line closes the fence — HTML bodies escape backticks as &#96;
// per the authoring contract, so a raw run can only be the close. Legacy JSON
// bodies keep the strict own-line close (their string values may carry ```).
func ExtractFences(text string) []string {
	var out []string
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		rest, open := denebUIFenceOpenSplit(strings.TrimSpace(lines[i]))
		if !open {
			continue
		}
		var body []string
		isHTML := rest != ""
		htmlDecided := rest != ""
		closed := false
		if rest != "" {
			if pre, ok := splitGluedFenceClose(rest); ok {
				body, closed = appendBodyLine(body, pre), true
			} else {
				body = append(body, rest)
			}
		}
		for !closed && i+1 < len(lines) {
			i++
			t := strings.TrimSpace(lines[i])
			if isFenceClose(t) {
				break
			}
			if !htmlDecided && t != "" {
				isHTML = strings.HasPrefix(t, "<")
				htmlDecided = true
			}
			if isHTML {
				if pre, ok := splitGluedFenceClose(lines[i]); ok {
					body, closed = appendBodyLine(body, pre), true
					break
				}
			}
			body = append(body, lines[i])
		}
		out = append(out, strings.Join(body, "\n"))
	}
	return out
}

// HasFence reports whether text contains at least one deneb-ui block.
func HasFence(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if _, open := denebUIFenceOpenSplit(strings.TrimSpace(line)); open {
			return true
		}
	}
	return false
}

// IsFenceOpenLine reports whether a line opens a deneb-ui fence under the
// SAME contract the extractor applies (case-insensitive info string; a
// remainder only when it starts with '<'). Server-side consumers (the
// proactive relay's collapse bypass) must use this instead of a prefix check
// so they can never diverge from what the renderers will actually parse.
func IsFenceOpenLine(line string) bool {
	return isDenebUIFenceOpen(strings.TrimSpace(line))
}

// isDenebUIFenceOpen reports whether a (whitespace-trimmed) line opens a
// deneb-ui fence (see denebUIFenceOpenSplit for the tolerated shapes).
func isDenebUIFenceOpen(line string) bool {
	_, open := denebUIFenceOpenSplit(line)
	return open
}

// denebUIFenceOpenSplit recognizes a deneb-ui fence opener in a line and
// returns any body content glued after the info string. The fence normally
// occupies its own line, but models sometimes glue it to the tail of a prose
// sentence ("…할게요.```deneb-ui") or run straight into the first tag
// ("…```deneb-ui<column>"). A remainder is accepted only when it starts with
// '<' — prose that merely mentions the fence mid-sentence stays prose.
func denebUIFenceOpenSplit(line string) (rest string, open bool) {
	_, rest, open = denebUIFenceOpenParts(line)
	return rest, open
}

// denebUIFenceOpenParts is the lossless form used by fence rewriters. prefix
// is any prose before the opener and rest is same-line HTML after the info
// string. The public extractor only needs rest, so denebUIFenceOpenSplit keeps
// its smaller contract above.
func denebUIFenceOpenParts(line string) (prefix, rest string, open bool) {
	line = strings.TrimRight(line, " \t")
	for from := 0; ; {
		bt := strings.Index(line[from:], "```")
		if bt < 0 {
			return "", "", false
		}
		start := from + bt
		j := start
		for j < len(line) && line[j] == '`' {
			j++
		}
		k := j
		for k < len(line) && (line[k] == ' ' || line[k] == '\t') {
			k++
		}
		if len(line)-k >= len(FenceInfo) && strings.EqualFold(line[k:k+len(FenceInfo)], FenceInfo) {
			m := k + len(FenceInfo)
			for m < len(line) && (line[m] == ' ' || line[m] == '\t') {
				m++
			}
			if m == len(line) {
				return strings.TrimSpace(line[:start]), "", true
			}
			if line[m] == '<' {
				return strings.TrimSpace(line[:start]), line[m:], true
			}
		}
		from = from + bt + 1
	}
}

// splitGluedFenceClose detects a ``` run glued into an HTML body line
// ("</column>``` 뒤 프로즈") and returns the body part before the run.
func splitGluedFenceClose(line string) (pre string, closed bool) {
	pre, _, closed = splitGluedFenceCloseParts(line)
	return pre, closed
}

// splitGluedFenceCloseParts is the lossless form for rewriters that must keep
// prose following an HTML fence close on the same line.
func splitGluedFenceCloseParts(line string) (pre, suffix string, closed bool) {
	bt := strings.Index(line, "```")
	if bt < 0 {
		return "", "", false
	}
	j := bt
	for j < len(line) && line[j] == '`' {
		j++
	}
	return line[:bt], strings.TrimSpace(line[j:]), true
}

// appendBodyLine appends a body fragment, skipping blank fragments.
func appendBodyLine(body []string, line string) []string {
	if strings.TrimSpace(line) == "" {
		return body
	}
	return append(body, line)
}

func isFenceClose(line string) bool {
	return strings.HasPrefix(line, "```") && strings.TrimSpace(strings.TrimLeft(line, "`")) == ""
}

// Validate structurally validates a deneb-ui block body against the
// DenebUiNode schema. Bodies starting with '<' use the labeled-HTML wire
// format (html.go — the authoring format since the 2026-07 JSON→HTML switch);
// '{'/'[' bodies take the legacy strict-JSON path, kept for old transcripts.
// It returns the list of issues (empty == valid). A non-nil error means the
// body was not parseable at all.
func Validate(body string) ([]Issue, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("empty deneb-ui block")
	}
	if IsHTMLBody(body) {
		root, issues := ParseHTML(body)
		if root == nil {
			return issues, nil
		}
		return append(issues, validateNode(root, "$")...), nil
	}
	var root any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		// Kai also accepts NDJSON (one object per line, wrapped in a column).
		nodes, nerr := parseNDJSON(body)
		if nerr != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		var issues []Issue
		for i, n := range nodes {
			issues = append(issues, validateNode(n, fmt.Sprintf("[%d]", i))...)
		}
		return issues, nil
	}
	return validateNode(root, "$"), nil
}

func validateNode(v any, path string) []Issue {
	switch t := v.(type) {
	case []any:
		var issues []Issue
		for i, e := range t {
			issues = append(issues, validateNode(e, fmt.Sprintf("%s[%d]", path, i))...)
		}
		return issues
	case map[string]any:
		return validateObject(t, path)
	default:
		return []Issue{{path, "expected a UI node object"}}
	}
}

func validateObject(m map[string]any, path string) []Issue {
	typ, _ := m["type"].(string)
	if typ == "" {
		return []Issue{{path, `missing or non-string "type"`}}
	}
	spec, ok := nodeSpecs[typ]
	if !ok {
		return []Issue{{path, fmt.Sprintf("unknown node type %q", typ)}}
	}

	var issues []Issue
	if spec.requireID {
		if id, _ := m["id"].(string); id == "" {
			issues = append(issues, Issue{path, fmt.Sprintf("node %q requires a non-empty %q", typ, "id")})
		}
	}
	for field, allowed := range spec.enums {
		if raw, present := m[field]; present {
			s, _ := raw.(string)
			if !contains(allowed, s) {
				issues = append(issues, Issue{
					path + "." + field,
					fmt.Sprintf("invalid %s %q (allowed: %s)", field, s, strings.Join(allowed, ", ")),
				})
			}
		}
	}
	for _, af := range spec.actionFields {
		if raw, present := m[af]; present && raw != nil {
			issues = append(issues, validateAction(raw, path+"."+af)...)
		}
	}
	for _, cf := range spec.childFields {
		raw, present := m[cf]
		if !present || raw == nil {
			continue
		}
		arr, ok := raw.([]any)
		if !ok {
			issues = append(issues, Issue{path + "." + cf, fmt.Sprintf("%q must be an array of nodes", cf)})
			continue
		}
		for i, e := range arr {
			issues = append(issues, validateNode(e, fmt.Sprintf("%s.%s[%d]", path, cf, i))...)
		}
	}
	if typ == "tabs" {
		issues = append(issues, validateTabs(m, path)...)
	}
	return issues
}

func validateTabs(m map[string]any, path string) []Issue {
	raw, present := m["tabs"]
	if !present || raw == nil {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return []Issue{{path + ".tabs", `"tabs" must be an array`}}
	}
	var issues []Issue
	for i, e := range arr {
		tab, ok := e.(map[string]any)
		if !ok {
			issues = append(issues, Issue{fmt.Sprintf("%s.tabs[%d]", path, i), "tab must be an object"})
			continue
		}
		ch, present := tab["children"]
		if !present || ch == nil {
			continue
		}
		carr, ok := ch.([]any)
		if !ok {
			issues = append(issues, Issue{fmt.Sprintf("%s.tabs[%d].children", path, i), "must be an array"})
			continue
		}
		for j, ce := range carr {
			issues = append(issues, validateNode(ce, fmt.Sprintf("%s.tabs[%d].children[%d]", path, i, j))...)
		}
	}
	return issues
}

func validateAction(v any, path string) []Issue {
	m, ok := v.(map[string]any)
	if !ok {
		return []Issue{{path, "action must be an object"}}
	}
	typ, _ := m["type"].(string)
	if typ == "" {
		return []Issue{{path, `action missing "type"`}}
	}
	if !contains(actionTypes, typ) {
		return []Issue{{path, fmt.Sprintf("unknown action type %q (allowed: %s)", typ, strings.Join(actionTypes, ", "))}}
	}
	return nil
}

func parseNDJSON(body string) ([]any, error) {
	var nodes []any
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var n any
		if err := json.Unmarshal([]byte(line), &n); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no JSON objects")
	}
	return nodes, nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
