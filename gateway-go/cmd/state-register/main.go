// state-register generates a cross-stage shared-state read/write map for one
// struct type — adapted from the Harness Handbook paper's state-register view
// (arXiv:2607.13285): call graphs answer "who calls this", but NOT "who else
// touches the state this change flows through". For a type like
// session.Session, whose fields are written in the domain state machine and
// read across runtime/pipeline packages, this map is the blast radius a
// symbol-level graph can't show.
//
// Dependency-free by design (no x/tools): a syntactic declared-type propagator
// tracks identifiers/fields/containers declared as the target type and follows
// selector/index chains from them. Bindings it cannot prove (type-inferred
// locals whose RHS it can't trace) are SKIPPED and counted, so the report is an
// honest under-approximation, never a guess.
//
// Usage:
//
//	go run ./cmd/state-register                # session.Session over ./internal/...
//	go run ./cmd/state-register -type internal/domain/session.Session -root ./internal
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type site struct {
	pkg   string
	file  string
	line  int
	fn    string
	write bool
}

type analyzer struct {
	fset       *token.FileSet
	typePkg    string // import-path suffix of the package declaring the type, e.g. internal/domain/session
	typeName   string // e.g. Session
	fields     map[string]bool
	sites      map[string][]site // field -> sites
	unresolved int
}

func main() {
	typeFlag := flag.String("type", "internal/domain/session.Session", "pkg-path-suffix.TypeName")
	root := flag.String("root", "./internal", "directory tree to scan")
	out := flag.String("out", "", "write markdown here (default stdout)")
	flag.Parse()

	dot := strings.LastIndex(*typeFlag, ".")
	if dot < 0 {
		fmt.Fprintln(os.Stderr, "-type must be pkgpath.TypeName")
		os.Exit(2)
	}
	a := &analyzer{
		fset:     token.NewFileSet(),
		typePkg:  (*typeFlag)[:dot],
		typeName: (*typeFlag)[dot+1:],
		fields:   map[string]bool{},
		sites:    map[string][]site{},
	}

	files, err := collectGoFiles(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	parsed := make(map[string]*ast.File, len(files))
	for _, f := range files {
		af, err := parser.ParseFile(a.fset, f, nil, 0)
		if err != nil {
			continue // unparseable source is skipped, not guessed
		}
		parsed[f] = af
	}

	// Pass 1: the target struct's field set, from its declaring package.
	for path, af := range parsed {
		if !strings.Contains(filepath.ToSlash(path), a.typePkg+"/") &&
			!strings.HasSuffix(filepath.ToSlash(filepath.Dir(path)), a.typePkg) {
			continue
		}
		a.harvestFields(af)
	}
	if len(a.fields) == 0 {
		fmt.Fprintf(os.Stderr, "type %s.%s not found under %s\n", a.typePkg, a.typeName, *root)
		os.Exit(1)
	}

	// Pass 2: every file — track declared bindings of the type, then classify
	// selector accesses reachable from them.
	for path, af := range parsed {
		a.scanFile(path, af)
	}

	md := a.render()
	if *out == "" {
		fmt.Print(md)
		return
	}
	if err := os.WriteFile(*out, []byte(md), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func collectGoFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

func (a *analyzer) harvestFields(af *ast.File) {
	for _, decl := range af.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != a.typeName {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, f := range st.Fields.List {
				for _, n := range f.Names {
					a.fields[n.Name] = true
				}
			}
		}
	}
}

// isTargetType reports whether a syntactic type expression denotes the target
// (Session / *Session / session.Session / *session.Session / containers of them).
// Container element matches let `m.sessions[k].Field` chains resolve.
func (a *analyzer) isTargetType(expr ast.Expr, samePkg bool) bool {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return a.isTargetType(t.X, samePkg)
	case *ast.Ident:
		return samePkg && t.Name == a.typeName
	case *ast.SelectorExpr:
		pkg, ok := t.X.(*ast.Ident)
		base := a.typePkg[strings.LastIndex(a.typePkg, "/")+1:]
		return ok && pkg.Name == base && t.Sel.Name == a.typeName
	case *ast.MapType:
		return a.isTargetType(t.Value, samePkg)
	case *ast.ArrayType:
		return a.isTargetType(t.Elt, samePkg)
	}
	return false
}

func (a *analyzer) scanFile(path string, af *ast.File) {
	slash := filepath.ToSlash(path)
	samePkg := strings.Contains(slash, a.typePkg+"/") ||
		strings.HasSuffix(filepath.ToSlash(filepath.Dir(path)), a.typePkg)
	pkg := filepath.ToSlash(filepath.Dir(path))
	if i := strings.Index(pkg, "internal/"); i >= 0 {
		pkg = pkg[i:]
	}

	// Declared-type table for this file: identifiers and (recv.field) chains
	// whose static type is the target — receivers, params, results, var decls,
	// and struct fields (incl. map/slice element types).
	typed := map[string]bool{}      // plain identifiers
	fieldTyped := map[string]bool{} // struct field names of target type (any owner)

	harvestSig := func(ft *ast.FuncType, recv *ast.FieldList) {
		lists := []*ast.FieldList{ft.Params, ft.Results, recv}
		for _, fl := range lists {
			if fl == nil {
				continue
			}
			for _, f := range fl.List {
				if !a.isTargetType(f.Type, samePkg) {
					continue
				}
				for _, n := range f.Names {
					typed[n.Name] = true
				}
			}
		}
	}
	ast.Inspect(af, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.FuncDecl:
			harvestSig(d.Type, d.Recv)
		case *ast.FuncLit:
			harvestSig(d.Type, nil)
		case *ast.ValueSpec:
			if d.Type != nil && a.isTargetType(d.Type, samePkg) {
				for _, name := range d.Names {
					typed[name.Name] = true
				}
			}
		case *ast.StructType:
			for _, f := range d.Fields.List {
				if a.isTargetType(f.Type, samePkg) {
					for _, name := range f.Names {
						fieldTyped[name.Name] = true
					}
				}
			}
		}
		return true
	})

	// baseIsTarget: does this expression statically denote a target value?
	var baseIsTarget func(e ast.Expr) bool
	baseIsTarget = func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.Ident:
			return typed[x.Name]
		case *ast.ParenExpr:
			return baseIsTarget(x.X)
		case *ast.StarExpr:
			return baseIsTarget(x.X)
		case *ast.UnaryExpr:
			return x.Op == token.AND && baseIsTarget(x.X)
		case *ast.IndexExpr: // m.sessions[k] where sessions is map/slice of target
			return baseIsTarget(x.X)
		case *ast.SelectorExpr: // recv.sessions — a field declared as target/container
			return fieldTyped[x.Sel.Name]
		case *ast.CompositeLit:
			return a.isTargetType(x.Type, samePkg)
		case *ast.CallExpr:
			return false // return-typed calls need real type info — skipped honestly
		}
		return false
	}

	writes := map[*ast.SelectorExpr]bool{}
	ast.Inspect(af, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range st.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok {
					writes[sel] = true
				}
			}
		case *ast.IncDecStmt:
			if sel, ok := st.X.(*ast.SelectorExpr); ok {
				writes[sel] = true
			}
		}
		return true
	})

	var fnStack []string
	pos := func(n ast.Node) (string, int) {
		p := a.fset.Position(n.Pos())
		return filepath.ToSlash(p.Filename), p.Line
	}
	ast.Inspect(af, func(n ast.Node) bool {
		if fd, ok := n.(*ast.FuncDecl); ok {
			fnStack = []string{fd.Name.Name}
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if !a.fields[sel.Sel.Name] {
			return true
		}
		if !baseIsTarget(sel.X) {
			// A field-name match whose base we can't prove — count, don't guess.
			if _, isIdent := sel.X.(*ast.Ident); isIdent {
				a.unresolved++
			}
			return true
		}
		file, line := pos(sel)
		fn := ""
		if len(fnStack) > 0 {
			fn = fnStack[len(fnStack)-1]
		}
		a.sites[sel.Sel.Name] = append(a.sites[sel.Sel.Name], site{
			pkg: pkg, file: file, line: line, fn: fn, write: writes[sel],
		})
		return true
	})
}

func (a *analyzer) render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# State register — %s.%s\n\n", a.typePkg, a.typeName)
	b.WriteString("<!-- GENERATED by cmd/state-register — DO NOT EDIT. Regenerate: make state-register -->\n\n")
	b.WriteString("이 표는 공유 상태 한 타입의 필드별 write/read 지점을 패키지 경계 너머까지 펼친다\n")
	b.WriteString("(Harness Handbook의 state-register 뷰 채택). 콜그래프가 못 보여주는 \"이 상태를\n")
	b.WriteString("바꾸면 어디가 영향받나\"의 블래스트 반경. 선언-타입 전파 기반의 정직한\n")
	b.WriteString("과소근사: 타입 추론 로컬 등 증명 못 한 접근은 세지 않고 아래에 개수만 보고한다.\n\n")

	fields := make([]string, 0, len(a.sites))
	for f := range a.sites {
		fields = append(fields, f)
	}
	sort.Strings(fields)

	for _, f := range fields {
		ss := a.sites[f]
		var w, r []site
		pkgs := map[string]bool{}
		for _, s := range ss {
			pkgs[s.pkg] = true
			if s.write {
				w = append(w, s)
			} else {
				r = append(r, s)
			}
		}
		cross := ""
		if len(pkgs) > 1 {
			cross = fmt.Sprintf(" · **크로스-패키지 %d개**", len(pkgs))
		}
		fmt.Fprintf(&b, "## %s — write %d · read %d%s\n\n", f, len(w), len(r), cross)
		emit := func(label string, sites []site) {
			if len(sites) == 0 {
				return
			}
			sort.Slice(sites, func(i, j int) bool {
				if sites[i].file != sites[j].file {
					return sites[i].file < sites[j].file
				}
				return sites[i].line < sites[j].line
			})
			fmt.Fprintf(&b, "%s:\n\n", label)
			for _, s := range sites {
				fmt.Fprintf(&b, "- `%s:%d` %s\n", s.file, s.line, s.fn)
			}
			b.WriteString("\n")
		}
		emit("**writes**", w)
		emit("reads", r)
	}
	fmt.Fprintf(&b,
		"---\n\n필드명은 일치하나 타입을 증명하지 못해 제외한 접근: %d건 — 다른 타입의\n"+
			"동명 필드(진짜 무관)와 타입 추론 로컬(놓친 접근)이 섞인 상한이다.\n",
		a.unresolved)
	return b.String()
}
