// state-register generates a cross-stage shared-state read/write map for one
// struct type — adapted from the Harness Handbook paper's state-register view
// (arXiv:2607.13285): call graphs answer "who calls this", but NOT "who else
// touches the state this change flows through". For a type like
// session.Session, whose fields are written in the domain state machine and
// read across runtime/pipeline packages, this map is the blast radius a
// symbol-level graph can't show.
//
// Dependency-free by design (no x/tools). v2 runs the real stdlib go/types
// checker per package (imports resolved from gc export data via
// `go list -export`), so field accesses through return-typed calls, closures,
// promoted fields, and inferred locals all resolve exactly — the v1 syntactic
// propagator's under-approximation is gone.
//
// Usage:
//
//	go run ./cmd/state-register                # session.Session over ./internal/...
//	go run ./cmd/state-register -type internal/domain/workfeed.Item -out map.md
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
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

type register struct {
	typePkg    string // import-path suffix declaring the type, e.g. internal/domain/session
	typeName   string // e.g. Session
	fields     map[string]bool
	sites      map[string][]site
	unresolved int // field-name matches the checker could not resolve (check errors)
}

func main() {
	typeFlag := flag.String("type", "internal/domain/session.Session", "pkg-path-suffix.TypeName")
	root := flag.String("root", "./internal", "package tree to scan")
	out := flag.String("out", "", "write markdown here (default stdout)")
	flag.Parse()

	dot := strings.LastIndex(*typeFlag, ".")
	if dot < 0 {
		fmt.Fprintln(os.Stderr, "-type must be pkgpath.TypeName")
		os.Exit(2)
	}
	r := &register{
		typePkg:  (*typeFlag)[:dot],
		typeName: (*typeFlag)[dot+1:],
		fields:   map[string]bool{},
		sites:    map[string][]site{},
	}

	pattern := strings.TrimSuffix(*root, "/") + "/..."
	pkgs, exports, err := loadPackages(pattern)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fset := token.NewFileSet()
	for _, p := range pkgs {
		files, info, err := checkPackage(fset, p, exports)
		if err != nil {
			continue
		}
		if strings.HasSuffix(p.ImportPath, r.typePkg) {
			r.harvestFields(info)
		}
		r.scanPackage(fset, p.ImportPath, files, info)
	}
	if len(r.fields) == 0 {
		fmt.Fprintf(os.Stderr, "type %s.%s not found under %s\n", r.typePkg, r.typeName, *root)
		os.Exit(1)
	}

	md := r.render()
	if *out == "" {
		fmt.Print(md)
		return
	}
	if err := os.WriteFile(*out, []byte(md), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// harvestFields pulls the target struct's field-name set from the checked
// declaring package.
func (r *register) harvestFields(info *types.Info) {
	for _, obj := range info.Defs {
		tn, ok := obj.(*types.TypeName)
		if !ok || tn.Name() != r.typeName {
			continue
		}
		st, ok := tn.Type().Underlying().(*types.Struct)
		if !ok {
			continue
		}
		for i := range st.NumFields() {
			r.fields[st.Field(i).Name()] = true
		}
	}
}

// scanPackage records every selection whose receiver the checker proved to be
// the target type. Selections the checker could not resolve (packages with
// check errors) are counted, not guessed.
func (r *register) scanPackage(fset *token.FileSet, importPath string, files []*ast.File, info *types.Info) {
	pkg := importPath
	if i := strings.Index(pkg, "internal/"); i >= 0 {
		pkg = pkg[i:]
	}

	writes := map[*ast.SelectorExpr]bool{}
	for _, af := range files {
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
	}

	for _, af := range files {
		var fn string
		ast.Inspect(af, func(n ast.Node) bool {
			if fd, ok := n.(*ast.FuncDecl); ok {
				fn = fd.Name.Name
			}
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			s, resolved := info.Selections[sel]
			if !resolved {
				// Unresolved selector that at least names a target field —
				// only possible when this package failed to fully check.
				if r.fields[sel.Sel.Name] && info.Uses[sel.Sel] == nil {
					r.unresolved++
				}
				return true
			}
			if s.Kind() != types.FieldVal || !recvIsTarget(s, r.typePkg, r.typeName) {
				return true
			}
			if !r.fields[sel.Sel.Name] {
				return true
			}
			p := fset.Position(sel.Pos())
			file := p.Filename
			if i := strings.Index(file, "gateway-go/"); i >= 0 {
				file = file[i:]
			}
			r.sites[sel.Sel.Name] = append(r.sites[sel.Sel.Name], site{
				pkg: pkg, file: file, line: p.Line, fn: fn, write: writes[sel],
			})
			return true
		})
	}
}

func (r *register) render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# State register — %s.%s\n\n", r.typePkg, r.typeName)
	b.WriteString("<!-- GENERATED by cmd/state-register — DO NOT EDIT. Regenerate: make state-register -->\n\n")
	b.WriteString("이 표는 공유 상태 한 타입의 필드별 write/read 지점을 패키지 경계 너머까지 펼친다\n")
	b.WriteString("(Harness Handbook의 state-register 뷰 채택). 콜그래프가 못 보여주는 \"이 상태를\n")
	b.WriteString("바꾸면 어디가 영향받나\"의 블래스트 반경. go/types 타입체크 기반 — 리턴 타입\n")
	b.WriteString("호출·클로저·추론 로컬을 지나는 접근까지 컴파일러가 해석하는 그대로 계수한다.\n\n")

	fields := make([]string, 0, len(r.sites))
	for f := range r.sites {
		fields = append(fields, f)
	}
	sort.Strings(fields)

	for _, f := range fields {
		ss := r.sites[f]
		var w, rd []site
		pkgs := map[string]bool{}
		for _, s := range ss {
			pkgs[s.pkg] = true
			if s.write {
				w = append(w, s)
			} else {
				rd = append(rd, s)
			}
		}
		cross := ""
		if len(pkgs) > 1 {
			cross = fmt.Sprintf(" · **크로스-패키지 %d개**", len(pkgs))
		}
		fmt.Fprintf(&b, "## %s — write %d · read %d%s\n\n", f, len(w), len(rd), cross)
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
		emit("reads", rd)
	}
	fmt.Fprintf(&b,
		"---\n\n타입체커가 해석하지 못한 필드명 일치 접근: %d건 (체크 에러가 있는 패키지에서만 발생).\n",
		r.unresolved)
	return b.String()
}
