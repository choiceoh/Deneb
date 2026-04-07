package repl

import (
	"context"
	"fmt"
	"strings"

	"go.starlark.net/starlark"
)

// WikiReadFunc reads a wiki page by relative path and returns its full content.
type WikiReadFunc func(relPath string) (string, error)

// WikiReadBatchFunc reads multiple wiki pages in parallel.
type WikiReadBatchFunc func(relPaths []string) ([]string, error)

// WikiListFunc lists all page paths, optionally filtered by category.
type WikiListFunc func(category string) ([]string, error)

// WikiIndexFunc returns the rendered index (TSV format), optionally for one category.
type WikiIndexFunc func(category string) (string, error)

// WikiSearchFunc performs FTS5 search and returns formatted results.
type WikiSearchFunc func(ctx context.Context, query string, limit int) (string, error)

// WikiWriteFunc writes a page to the wiki.
type WikiWriteFunc func(relPath, content string) error

// WikiFuncs groups all wiki callback functions for REPL injection.
type WikiFuncs struct {
	Read      WikiReadFunc
	ReadBatch WikiReadBatchFunc
	List      WikiListFunc
	Index     WikiIndexFunc
	Search    WikiSearchFunc
	Write     WikiWriteFunc
}

// registerWikiBuiltins adds wiki_* functions to the Starlark globals.
func registerWikiBuiltins(ctx context.Context, globals starlark.StringDict, wf WikiFuncs) {
	if wf.Read != nil {
		globals["wiki_read"] = builtinWikiRead(wf.Read)
	}
	if wf.ReadBatch != nil {
		globals["wiki_read_batch"] = builtinWikiReadBatch(wf.ReadBatch)
	}
	if wf.List != nil {
		globals["wiki_list"] = builtinWikiList(wf.List)
	}
	if wf.Index != nil {
		globals["wiki_index"] = builtinWikiIndex(wf.Index)
	}
	if wf.Search != nil {
		globals["wiki_search"] = builtinWikiSearch(ctx, wf.Search)
	}
	if wf.Write != nil {
		globals["wiki_write"] = builtinWikiWrite(wf.Write)
	}
}

func builtinWikiRead(fn WikiReadFunc) *starlark.Builtin {
	return starlark.NewBuiltin("wiki_read", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var path starlark.String
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &path); err != nil {
			return starlark.None, err
		}
		content, err := fn(string(path))
		if err != nil {
			return starlark.None, fmt.Errorf("wiki_read: %w", err)
		}
		return starlark.String(content), nil
	})
}

func builtinWikiReadBatch(fn WikiReadBatchFunc) *starlark.Builtin {
	return starlark.NewBuiltin("wiki_read_batch", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var pathList starlark.Value
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &pathList); err != nil {
			return starlark.None, err
		}
		paths, err := starlarkToStringSlice(pathList)
		if err != nil {
			return starlark.None, fmt.Errorf("wiki_read_batch: %w", err)
		}
		contents, err := fn(paths)
		if err != nil {
			return starlark.None, fmt.Errorf("wiki_read_batch: %w", err)
		}
		elems := make([]starlark.Value, len(contents))
		for i, c := range contents {
			elems[i] = starlark.String(c)
		}
		return starlark.NewList(elems), nil
	})
}

func builtinWikiList(fn WikiListFunc) *starlark.Builtin {
	return starlark.NewBuiltin("wiki_list", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var category starlark.String
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0, &category); err != nil {
			return starlark.None, err
		}
		pages, err := fn(string(category))
		if err != nil {
			return starlark.None, fmt.Errorf("wiki_list: %w", err)
		}
		elems := make([]starlark.Value, len(pages))
		for i, p := range pages {
			elems[i] = starlark.String(p)
		}
		return starlark.NewList(elems), nil
	})
}

func builtinWikiIndex(fn WikiIndexFunc) *starlark.Builtin {
	return starlark.NewBuiltin("wiki_index", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var category starlark.String
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 0, &category); err != nil {
			return starlark.None, err
		}
		content, err := fn(string(category))
		if err != nil {
			return starlark.None, fmt.Errorf("wiki_index: %w", err)
		}
		return starlark.String(content), nil
	})
}

func builtinWikiSearch(ctx context.Context, fn WikiSearchFunc) *starlark.Builtin {
	return starlark.NewBuiltin("wiki_search", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var query starlark.String
		limit := starlark.MakeInt(10)
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 1, &query, &limit); err != nil {
			return starlark.None, err
		}
		n, _ := starlark.AsInt32(limit)
		if n <= 0 {
			n = 10
		}
		results, err := fn(ctx, string(query), int(n))
		if err != nil {
			return starlark.None, fmt.Errorf("wiki_search: %w", err)
		}
		return starlark.String(results), nil
	})
}

func builtinWikiWrite(fn WikiWriteFunc) *starlark.Builtin {
	return starlark.NewBuiltin("wiki_write", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var path, content starlark.String
		if err := starlark.UnpackPositionalArgs(b.Name(), args, kwargs, 2, &path, &content); err != nil {
			return starlark.None, err
		}
		p := string(path)
		if !strings.HasSuffix(p, ".md") {
			p += ".md"
		}
		if err := fn(p, string(content)); err != nil {
			return starlark.None, fmt.Errorf("wiki_write: %w", err)
		}
		return starlark.String("ok: " + p), nil
	})
}

// wikiBuiltinNames lists wiki builtin names for SHOW_VARS exclusion.
var wikiBuiltinNames = []string{
	"wiki_read", "wiki_read_batch", "wiki_list", "wiki_index", "wiki_search", "wiki_write",
}

func init() {
	for _, name := range wikiBuiltinNames {
		builtinNames[name] = true
	}
}
