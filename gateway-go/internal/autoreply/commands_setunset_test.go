package autoreply

import (
	"testing"
)

func TestParseSetUnsetCommand(t *testing.T) {
	// Set with valid path=value.
	r := ParseSetUnsetCommand("/debug", "set", "foo=42")
	if r.Kind != SetUnsetSet || r.Path != "foo" {
		t.Errorf("set: got %+v", r)
	}
	if v, ok := r.Value.(float64); !ok || v != 42 {
		t.Errorf("set value: got %v", r.Value)
	}

	// Set with missing args.
	r = ParseSetUnsetCommand("/debug", "set", "")
	if r.Kind != SetUnsetError {
		t.Errorf("set empty: got %+v", r)
	}

	// Set without equals.
	r = ParseSetUnsetCommand("/debug", "set", "foobar")
	if r.Kind != SetUnsetError {
		t.Errorf("set no eq: got %+v", r)
	}

	// Unset with path.
	r = ParseSetUnsetCommand("/debug", "unset", "foo.bar")
	if r.Kind != SetUnsetUnset || r.Path != "foo.bar" {
		t.Errorf("unset: got %+v", r)
	}

	// Unset without path.
	r = ParseSetUnsetCommand("/debug", "unset", "")
	if r.Kind != SetUnsetError {
		t.Errorf("unset empty: got %+v", r)
	}
}

func TestParseSlashCommandWithSetUnset(t *testing.T) {
	type result struct {
		action  string
		path    string
		message string
	}

	parse := func(raw string) (result, bool) {
		return ParseSlashCommandWithSetUnset(SetUnsetSlashParams[result]{
			Raw:            raw,
			Slash:          "/debug",
			InvalidMessage: "invalid",
			UsageMessage:   "usage",
			OnKnownAction: func(action, _ string) (result, bool) {
				if action == "show" {
					return result{action: "show"}, true
				}
				return result{}, false
			},
			OnSet:   func(p string, _ any) result { return result{action: "set", path: p} },
			OnUnset: func(p string) result { return result{action: "unset", path: p} },
			OnError: func(m string) result { return result{action: "error", message: m} },
		})
	}

	// Non-matching.
	_, ok := parse("hello")
	if ok {
		t.Error("should not match non-slash")
	}

	// Known action.
	r, ok := parse("/debug show")
	if !ok || r.action != "show" {
		t.Errorf("show: %+v", r)
	}

	// Set.
	r, ok = parse("/debug set x=1")
	if !ok || r.action != "set" || r.path != "x" {
		t.Errorf("set: %+v", r)
	}

	// Unset.
	r, ok = parse("/debug unset x")
	if !ok || r.action != "unset" || r.path != "x" {
		t.Errorf("unset: %+v", r)
	}

	// Unknown action falls to usage.
	r, ok = parse("/debug unknown")
	if !ok || r.action != "error" || r.message != "usage" {
		t.Errorf("unknown: %+v", r)
	}
}
