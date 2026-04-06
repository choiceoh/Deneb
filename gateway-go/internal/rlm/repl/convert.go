// Package repl implements a Starlark-based REPL environment for RLM
// (Recursive Language Models). It provides an in-process, sandboxed
// execution environment where LLMs can write Python-like code to explore
// conversation history, call sub-LLMs, and produce final answers.
package repl

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// MessageEntry is a single conversation message exposed to Starlark as a dict.
type MessageEntry struct {
	Seq       int    `json:"seq"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
}

// messagesToStarlark converts a Go slice of messages into a Starlark list
// of struct-like dicts for efficient field access.
func messagesToStarlark(msgs []MessageEntry) *starlark.List {
	elems := make([]starlark.Value, len(msgs))
	for i, m := range msgs {
		elems[i] = starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
			"seq":        starlark.MakeInt(m.Seq),
			"role":       starlark.String(m.Role),
			"content":    starlark.String(m.Content),
			"created_at": starlark.MakeInt64(m.CreatedAt),
		})
	}
	return starlark.NewList(elems)
}

// starlarkToString converts a Starlark value to a Go string.
// Starlark strings are unquoted; other values use repr.
func starlarkToString(v starlark.Value) string {
	if s, ok := v.(starlark.String); ok {
		return string(s)
	}
	return v.String()
}

// starlarkToStringSlice converts a Starlark list/tuple to a Go string slice.
func starlarkToStringSlice(v starlark.Value) ([]string, error) {
	iter, ok := v.(starlark.Iterable)
	if !ok {
		return nil, fmt.Errorf("expected iterable, got %s", v.Type())
	}
	it := iter.Iterate()
	defer it.Done()

	var out []string
	var elem starlark.Value
	for it.Next(&elem) {
		out = append(out, starlarkToString(elem))
	}
	return out, nil
}
