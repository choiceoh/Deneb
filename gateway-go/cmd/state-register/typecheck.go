package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Type-checked analysis backend. Precision note: the original engine was a
// syntactic declared-type propagator (an honest under-approximation — it
// skipped type-inferred locals it could not trace). This backend runs the real
// go/types checker instead, resolving every selection the compiler resolves:
// return-typed calls, interface method values, promoted fields, closures.
// Still dependency-free: dependencies import as gc export data discovered via
// `go list -export -deps -json`, wired into the stdlib importer's lookup hook.

type listPkg struct {
	ImportPath string
	Dir        string
	Export     string
	GoFiles    []string
	Standard   bool
	Module     *struct{ Path string }
}

// loadPackages returns all packages of `pattern` plus an importPath→export-data
// lookup covering their full dependency closure.
func loadPackages(pattern string) (mod []listPkg, exports map[string]string, err error) {
	cmd := exec.CommandContext(context.Background(), "go", "list", "-e", "-export", "-deps", "-json=ImportPath,Dir,Export,GoFiles,Standard,Module", pattern)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("go list -export: %w", err)
	}
	exports = map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listPkg
		if err := dec.Decode(&p); err != nil {
			if err == io.EOF {
				break
			}
			return nil, nil, err
		}
		if p.Export != "" {
			exports[p.ImportPath] = p.Export
		}
		if !p.Standard && p.Module != nil && matchesPattern(p.ImportPath, pattern) {
			mod = append(mod, p)
		}
	}
	return mod, exports, nil
}

func matchesPattern(importPath, pattern string) bool {
	// pattern is "./internal/..." relative to gateway-go; module packages under
	// it carry the module prefix — suffix-match on the relative part.
	rel := strings.TrimPrefix(strings.TrimSuffix(pattern, "/..."), "./")
	return strings.Contains(importPath, "/"+rel+"/") || strings.HasSuffix(importPath, "/"+rel)
}

// checkPackage parses and type-checks one package from source, resolving
// imports through the export-data lookup. Check errors are tolerated: the
// checker still populates Info for everything it could resolve.
func checkPackage(fset *token.FileSet, p listPkg, exports map[string]string) ([]*ast.File, *types.Info, error) {
	var files []*ast.File
	for _, f := range p.GoFiles {
		af, err := parser.ParseFile(fset, p.Dir+"/"+f, nil, 0)
		if err != nil {
			continue
		}
		files = append(files, af)
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no parseable files")
	}
	lookup := func(path string) (io.ReadCloser, error) {
		exp, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(exp)
	}
	conf := types.Config{
		Importer: importer.ForCompiler(fset, "gc", lookup),
		Error:    func(error) {}, // tolerate; Info stays usable for resolved parts
	}
	info := &types.Info{
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Uses:       map[*ast.Ident]types.Object{},
		Defs:       map[*ast.Ident]types.Object{},
		Types:      map[ast.Expr]types.TypeAndValue{},
	}
	_, _ = conf.Check(p.ImportPath, fset, files, info) // error intentionally ignored (Error hook set)
	return files, info, nil
}

// recvIsTarget reports whether a selection's receiver (after pointer deref)
// is the target named type.
func recvIsTarget(sel *types.Selection, typePkgSuffix, typeName string) bool {
	t := sel.Recv()
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Name() == typeName &&
		strings.HasSuffix(named.Obj().Pkg().Path(), typePkgSuffix)
}
