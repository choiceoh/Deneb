package repl

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// LLMQueryFunc calls a sub-LLM with a prompt and returns the response text.
type LLMQueryFunc func(ctx context.Context, prompt string) (string, error)

// LLMBatchFunc calls sub-LLMs in parallel and returns response texts.
type LLMBatchFunc func(ctx context.Context, prompts []string) ([]string, error)

// RLMQueryFunc spawns a recursive RLM with its own REPL and context.
type RLMQueryFunc func(ctx context.Context, prompt string, subContext []MessageEntry) (string, error)

// builtinPrint creates a print() function that captures output to a buffer.
func builtinPrint(buf *bytes.Buffer, mu *sync.Mutex) *starlark.Builtin {
	return starlark.NewBuiltin("print", func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		mu.Lock()
		defer mu.Unlock()

		sep := " "
		end := "\n"
		for _, kv := range kwargs {
			switch string(kv[0].(starlark.String)) {
			case "sep":
				sep = starlarkToString(kv[1])
			case "end":
				end = starlarkToString(kv[1])
			}
		}

		for i, arg := range args {
			if i > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(starlarkToString(arg))
		}
		buf.WriteString(end)
		return starlark.None, nil
	})
}

// builtinLLMQuery creates the llm_query(prompt) builtin.
func builtinLLMQuery(ctx context.Context, fn LLMQueryFunc) *starlark.Builtin {
	return starlark.NewBuiltin("llm_query", func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var prompt starlark.String
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &prompt); err != nil {
			return starlark.None, err
		}
		result, err := fn(ctx, string(prompt))
		if err != nil {
			return starlark.None, fmt.Errorf("llm_query: %w", err)
		}
		return starlark.String(result), nil
	})
}

// builtinLLMBatch creates the llm_query_batch(prompts) builtin.
func builtinLLMBatch(ctx context.Context, fn LLMBatchFunc) *starlark.Builtin {
	return starlark.NewBuiltin("llm_query_batch", func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var promptList starlark.Value
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &promptList); err != nil {
			return starlark.None, err
		}
		prompts, err := starlarkToStringSlice(promptList)
		if err != nil {
			return starlark.None, fmt.Errorf("llm_query_batch: %w", err)
		}
		if len(prompts) == 0 {
			return starlark.NewList(nil), nil
		}
		results, err := fn(ctx, prompts)
		if err != nil {
			return starlark.None, fmt.Errorf("llm_query_batch: %w", err)
		}
		elems := make([]starlark.Value, len(results))
		for i, r := range results {
			elems[i] = starlark.String(r)
		}
		return starlark.NewList(elems), nil
	})
}

// builtinRLMQuery creates the rlm_query(prompt, sub_context) builtin.
func builtinRLMQuery(ctx context.Context, fn RLMQueryFunc, env *Env) *starlark.Builtin {
	return starlark.NewBuiltin("rlm_query", func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var prompt starlark.String
		var subCtx starlark.Value
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 2, &prompt, &subCtx); err != nil {
			return starlark.None, err
		}
		// Convert sub_context from Starlark list back to Go messages.
		msgs, err := starlarkListToMessages(subCtx)
		if err != nil {
			return starlark.None, fmt.Errorf("rlm_query: invalid sub_context: %w", err)
		}
		result, err := fn(ctx, string(prompt), msgs)
		if err != nil {
			return starlark.None, fmt.Errorf("rlm_query: %w", err)
		}
		return starlark.String(result), nil
	})
}

// starlarkListToMessages converts a Starlark list of structs back to Go messages.
func starlarkListToMessages(v starlark.Value) ([]MessageEntry, error) {
	list, ok := v.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("expected list, got %s", v.Type())
	}
	msgs := make([]MessageEntry, list.Len())
	for i := 0; i < list.Len(); i++ {
		elem := list.Index(i)
		msg, err := structToMessage(elem)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		msgs[i] = msg
	}
	return msgs, nil
}

func structToMessage(v starlark.Value) (MessageEntry, error) {
	s, ok := v.(*starlarkstruct.Struct)
	if !ok {
		return MessageEntry{}, fmt.Errorf("expected struct, got %s", v.Type())
	}
	seqV, _ := s.Attr("seq")
	roleV, _ := s.Attr("role")
	contentV, _ := s.Attr("content")
	createdV, _ := s.Attr("created_at")

	seq, _ := starlark.AsInt32(seqV)
	createdAt, _ := starlark.AsInt32(createdV)

	return MessageEntry{
		Seq:       int(seq),
		Role:      starlarkToString(roleV),
		Content:   starlarkToString(contentV),
		CreatedAt: int64(createdAt),
	}, nil
}

// builtinFinal creates the FINAL(answer) builtin that marks the final answer.
func builtinFinal(finalVal **string) *starlark.Builtin {
	return starlark.NewBuiltin("FINAL", func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var answer starlark.String
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &answer); err != nil {
			return starlark.None, err
		}
		s := string(answer)
		*finalVal = &s
		return starlark.None, nil
	})
}

// builtinFinalVar creates the FINAL_VAR(var_name) builtin that returns
// the value of a variable from the REPL environment as the final answer.
func builtinFinalVar(env *Env) *starlark.Builtin {
	return starlark.NewBuiltin("FINAL_VAR", func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var varName starlark.String
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &varName); err != nil {
			return starlark.None, err
		}
		name := string(varName)
		val, ok := env.globals[name]
		if !ok {
			return starlark.None, fmt.Errorf("variable %q not found", name)
		}
		s := starlarkToString(val)
		env.finalVal = &s
		return starlark.None, nil
	})
}

// builtinRegexSearch creates regex_search(pattern, text) → list of matches.
func builtinRegexSearch() *starlark.Builtin {
	return starlark.NewBuiltin("regex_search", func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var pattern, text starlark.String
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 2, &pattern, &text); err != nil {
			return starlark.None, err
		}
		re, err := regexp.Compile(string(pattern))
		if err != nil {
			return starlark.None, fmt.Errorf("regex_search: invalid pattern: %w", err)
		}
		match := re.FindString(string(text))
		if match == "" {
			return starlark.None, nil
		}
		return starlark.String(match), nil
	})
}

// builtinRegexFindall creates regex_findall(pattern, text) → list of strings.
func builtinRegexFindall() *starlark.Builtin {
	return starlark.NewBuiltin("regex_findall", func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var pattern, text starlark.String
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 2, &pattern, &text); err != nil {
			return starlark.None, err
		}
		re, err := regexp.Compile(string(pattern))
		if err != nil {
			return starlark.None, fmt.Errorf("regex_findall: invalid pattern: %w", err)
		}
		matches := re.FindAllString(string(text), -1)
		elems := make([]starlark.Value, len(matches))
		for i, m := range matches {
			elems[i] = starlark.String(m)
		}
		return starlark.NewList(elems), nil
	})
}

// builtinLen creates a len() function that handles Starlark strings by
// counting Unicode code points instead of bytes, matching Python behavior.
func builtinLen() *starlark.Builtin {
	return starlark.NewBuiltin("len", func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var x starlark.Value
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &x); err != nil {
			return starlark.None, err
		}
		if s, ok := x.(starlark.String); ok {
			return starlark.MakeInt(len(strings.ToValidUTF8(string(s), ""))), nil
		}
		// Delegate to Starlark's built-in len for lists, dicts, etc.
		return starlark.None, fmt.Errorf("len: unsupported type %s", x.Type())
	})
}

// builtinStr creates a str() function for explicit string conversion.
func builtinStr() *starlark.Builtin {
	return starlark.NewBuiltin("str", func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var x starlark.Value
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &x); err != nil {
			return starlark.None, err
		}
		return starlark.String(starlarkToString(x)), nil
	})
}

// builtinShowVars creates SHOW_VARS() that lists all user-defined variables.
func builtinShowVars(env *Env) *starlark.Builtin {
	return starlark.NewBuiltin("SHOW_VARS", func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var names []string
		for name := range env.globals {
			if !isBuiltinName(name) {
				names = append(names, name)
			}
		}
		elems := make([]starlark.Value, len(names))
		for i, n := range names {
			elems[i] = starlark.String(n)
		}
		return starlark.NewList(elems), nil
	})
}

// builtinNames lists names reserved for builtins (not shown in SHOW_VARS).
var builtinNames = map[string]bool{
	"context": true, "llm_query": true, "llm_query_batch": true,
	"rlm_query": true, "regex_search": true, "regex_findall": true,
	"FINAL": true, "FINAL_VAR": true, "SHOW_VARS": true,
	"print": true, "len": true, "str": true, "True": true, "False": true, "None": true,
	"int": true, "float": true, "list": true, "dict": true, "tuple": true,
	"bool": true, "range": true, "enumerate": true, "zip": true,
	"sorted": true, "reversed": true, "min": true, "max": true, "sum": true,
	"abs": true, "hasattr": true, "getattr": true, "type": true, "repr": true,
}

func isBuiltinName(name string) bool {
	return builtinNames[name]
}
