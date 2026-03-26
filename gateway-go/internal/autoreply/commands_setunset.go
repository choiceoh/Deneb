// commands_setunset.go — Core set/unset command parsing.
// Mirrors src/auto-reply/reply/commands-setunset.ts (101 LOC).
package autoreply

import (
	"fmt"
	"strings"
)

// SetUnsetKind categorizes a set/unset parse result.
type SetUnsetKind int

const (
	SetUnsetSet SetUnsetKind = iota
	SetUnsetUnset
	SetUnsetError
)

// SetUnsetParseResult holds the parsed set or unset outcome.
type SetUnsetParseResult struct {
	Kind    SetUnsetKind
	Path    string
	Value   any
	Message string // set when Kind == SetUnsetError
}

// ParseSetUnsetCommand parses set/unset arguments for a given slash command.
func ParseSetUnsetCommand(slash, action, args string) SetUnsetParseResult {
	trimmedArgs := strings.TrimSpace(args)
	if action == "unset" {
		if trimmedArgs == "" {
			return SetUnsetParseResult{
				Kind:    SetUnsetError,
				Message: fmt.Sprintf("Usage: %s unset path", slash),
			}
		}
		return SetUnsetParseResult{Kind: SetUnsetUnset, Path: trimmedArgs}
	}
	// action == "set"
	if trimmedArgs == "" {
		return SetUnsetParseResult{
			Kind:    SetUnsetError,
			Message: fmt.Sprintf("Usage: %s set path=value", slash),
		}
	}
	eqIndex := strings.Index(trimmedArgs, "=")
	if eqIndex <= 0 {
		return SetUnsetParseResult{
			Kind:    SetUnsetError,
			Message: fmt.Sprintf("Usage: %s set path=value", slash),
		}
	}
	path := strings.TrimSpace(trimmedArgs[:eqIndex])
	rawValue := trimmedArgs[eqIndex+1:]
	if path == "" {
		return SetUnsetParseResult{
			Kind:    SetUnsetError,
			Message: fmt.Sprintf("Usage: %s set path=value", slash),
		}
	}
	parsed := ParseConfigValue(rawValue)
	if parsed.Error != "" {
		return SetUnsetParseResult{Kind: SetUnsetError, Message: parsed.Error}
	}
	return SetUnsetParseResult{Kind: SetUnsetSet, Path: path, Value: parsed.Value}
}

// SetUnsetCallbacks provides typed callbacks for set/unset/error outcomes.
type SetUnsetCallbacks[T any] struct {
	OnSet   func(path string, value any) T
	OnUnset func(path string) T
	OnError func(message string) T
}

// ParseSetUnsetCommandAction dispatches to callbacks based on the action.
// Returns (result, true) if the action was set/unset, or (zero, false) otherwise.
func ParseSetUnsetCommandAction[T any](slash, action, args string, cb SetUnsetCallbacks[T]) (T, bool) {
	if action != "set" && action != "unset" {
		var zero T
		return zero, false
	}
	parsed := ParseSetUnsetCommand(slash, action, args)
	switch parsed.Kind {
	case SetUnsetError:
		return cb.OnError(parsed.Message), true
	case SetUnsetSet:
		return cb.OnSet(parsed.Path, parsed.Value), true
	default:
		return cb.OnUnset(parsed.Path), true
	}
}

// ParseSlashCommandWithSetUnset parses a full slash command supporting
// set/unset actions plus custom known-action callbacks.
func ParseSlashCommandWithSetUnset[T any](params struct {
	Raw            string
	Slash          string
	InvalidMessage string
	UsageMessage   string
	OnKnownAction  func(action, args string) (T, bool)
	OnSet          func(path string, value any) T
	OnUnset        func(path string) T
	OnError        func(message string) T
}) (T, bool) {
	parsed := ParseSlashCommandOrNull(params.Raw, params.Slash, params.InvalidMessage, "")
	if parsed == nil {
		var zero T
		return zero, false
	}
	if !parsed.OK {
		return params.OnError(parsed.Message), true
	}
	// Try set/unset first.
	result, ok := ParseSetUnsetCommandAction(params.Slash, parsed.Action, parsed.Args, SetUnsetCallbacks[T]{
		OnSet:   params.OnSet,
		OnUnset: params.OnUnset,
		OnError: params.OnError,
	})
	if ok {
		return result, true
	}
	// Try known action.
	if known, matched := params.OnKnownAction(parsed.Action, parsed.Args); matched {
		return known, true
	}
	// Fall back to usage error.
	return params.OnError(params.UsageMessage), true
}
