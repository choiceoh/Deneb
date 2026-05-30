// Package kaiui defines the schema for Kai-style interactive UI blocks that the
// agent emits inside a ```kai-ui fenced code block in its reply text. The native
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
package kaiui

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FenceInfo is the markdown fence info-string that marks a kai-ui block.
// Matched case-insensitively, mirroring Kai's BlockScanner.kt.
const FenceInfo = "kai-ui"

// Issue describes a single schema violation at a JSON-ish path.
type Issue struct {
	Path string
	Msg  string
}

func (is Issue) String() string { return is.Path + ": " + is.Msg }

// nodeSpec captures the structural rules for one node type.
type nodeSpec struct {
	requireID    bool                // true if "id" must be a non-empty string
	childFields  []string            // fields holding arrays of child nodes
	actionFields []string            // fields holding a UiAction object
	enums        map[string][]string // field -> allowed string values
}

var (
	textStyles      = []string{"headline", "title", "body", "caption"}
	buttonVariants  = []string{"filled", "outlined", "text", "tonal"}
	alertSeverities = []string{"info", "success", "warning", "error"}
	actionTypes     = []string{"callback", "toggle", "open_url", "copy_to_clipboard"}
)

// nodeSpecs is the registry of every known KaiUiNode type. A type absent here is
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
	"text":   {enums: map[string][]string{"style": textStyles}},
	"image":  {},
	"icon":   {},
	"code":   {},
	"quote":  {},
	"badge":  {},
	"stat":   {},
	"avatar": {},
	"table":  {}, // headers/rows are strings, not nodes
	// Interactive (id-bearing).
	"button":      {actionFields: []string{"action"}, enums: map[string][]string{"variant": buttonVariants}},
	"text_input":  {requireID: true},
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

// ExtractFences returns the raw bodies of every ```kai-ui fenced block in text,
// in document order. Fence info match is case-insensitive.
func ExtractFences(text string) []string {
	var out []string
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		if !isKaiUIFenceOpen(strings.TrimSpace(lines[i])) {
			continue
		}
		var body []string
		i++
		for i < len(lines) && !isFenceClose(strings.TrimSpace(lines[i])) {
			body = append(body, lines[i])
			i++
		}
		out = append(out, strings.Join(body, "\n"))
		// loop's i++ steps past the closing fence (or stays at end)
	}
	return out
}

// HasFence reports whether text contains at least one kai-ui block.
func HasFence(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if isKaiUIFenceOpen(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

func isKaiUIFenceOpen(line string) bool {
	if !strings.HasPrefix(line, "```") {
		return false
	}
	info := strings.TrimSpace(strings.TrimLeft(line, "`"))
	return strings.EqualFold(info, FenceInfo)
}

func isFenceClose(line string) bool {
	return strings.HasPrefix(line, "```") && strings.TrimSpace(strings.TrimLeft(line, "`")) == ""
}

// Validate parses a kai-ui block body as JSON and structurally validates it
// against the KaiUiNode schema. It returns the list of issues (empty == valid).
// A non-nil error means the body was not parseable as JSON or NDJSON at all.
func Validate(body string) ([]Issue, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("empty kai-ui block")
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
				issues = append(issues, Issue{path + "." + field,
					fmt.Sprintf("invalid %s %q (allowed: %s)", field, s, strings.Join(allowed, ", "))})
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
