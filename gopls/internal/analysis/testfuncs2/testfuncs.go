package testfuncs

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/edge"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/internal/analysis/analyzerutil"
)

//go:embed doc.go
var doc string

var Analyzer = &analysis.Analyzer{
	Name:     "testfuncs",
	Doc:      analyzerutil.MustExtractDoc(doc, "testfuncs"),
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
	URL:      "https://pkg.go.dev/golang.org/x/tools/gopls/internal/analysis/testfuncs",
}

type (
	Context struct {
		*analysis.Pass
		Inspect *inspector.Inspector
		TB      map[*types.Var]*Test
	}

	Test struct {
		parent   *Test
		kind     *types.TypeName
		name     string
		at       ast.Node
		tb       *types.Var
		tainted  []error
		children []*Test
	}

	analysisResult int
)

const (
	analysisOk analysisResult = iota
	analysisTainted
)

func run(pass *analysis.Pass) (any, error) {
	x := &Context{
		Pass:    pass,
		Inspect: pass.ResultOf[inspect.Analyzer].(*inspector.Inspector),
		TB:      map[*types.Var]*Test{},
	}
	for cur := range x.Inspect.Root().Children() {
		// We only care about test files
		if !strings.HasSuffix(x.Fset.Position(cur.Node().Pos()).Filename, "_test.go") {
			continue
		}

		for cur := range cur.Preorder((*ast.FuncDecl)(nil)) {
			decl := cur.Node().(*ast.FuncDecl)
			obj, ok := x.TypesInfo.Defs[decl.Name].(*types.Func)
			if !ok || !obj.Exported() {
				continue
			}

			// error.Error has empty Position, PkgPath, and ObjectPath.
			if obj.Pkg() == nil {
				continue
			}

			kind, ok := isTestOrExample(obj)
			if !ok {
				continue
			}

			body := cur.ChildAt(edge.FuncDecl_Body, -1)
			t := x.captureTest(decl.Name.Name, decl, kind, decl.Type, body)
			x.reportTest(t, "")
		}
	}

	return nil, nil
}

func (x *Context) captureTest(name string, at ast.Node, kind *types.TypeName, typ *ast.FuncType, body inspector.Cursor) *Test {
	// Don't recurse if we don't have a function type or body, or if this is an
	// example (kind == nil).
	t := &Test{name: name, kind: kind, at: at}
	if typ == nil || !body.Valid() || kind == nil {
		return t
	}

	// If the [testing.T] parameter is unnamed, the func cannot call
	// [testing.T.Run] and thus cannot create any subtests.
	if len(typ.Params.List) != 1 ||
		len(typ.Params.List[0].Names) == 0 {
		return t
	}

	// This "can't fail" because testKind should guarantee that the function has
	// one parameter and the check above guarantees that parameter is named
	t.tb = x.TypesInfo.Defs[typ.Params.List[0].Names[0]].(*types.Var)
	x.TB[t.tb] = t

	// Check for subtests.
	x.analyzeTest(t, body)
	return t
}

func (x *Context) analyzeTest(t *Test, cur inspector.Cursor) analysisResult {
	switch stmt := cur.Node().(type) {
	case *ast.BlockStmt:
		// Scan all of the statements. If one comes back tainted, stop.
		for cur := range cur.Children() {
			switch x.analyzeTest(t, cur) {
			case analysisTainted:
				return analysisTainted
			}
		}
		if t.isTainted() {
			return analysisTainted
		}
		return analysisOk

	case *ast.ExprStmt:
		// An ExprStmt must be a call or a channel receive. Handle the latter
		// via the default path.
		call, ok := stmt.X.(*ast.CallExpr)
		if !ok {
			break
		}

		// If this is not a TB method call, fall through.
		fun, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			break
		}
		recv, ok := fun.X.(*ast.Ident)
		if !ok {
			break
		}
		v, ok := x.TypesInfo.Uses[recv].(*types.Var)
		if !ok {
			break
		}

		// If tt is nil, that means this is NOT a TB call (or if it is, it's
		// somehow to an out-of-scope TB); in that case fall through.
		tt := t.findForVar(v)
		if tt == nil {
			break
		}

		// If this isn't a Run call, we don't care.
		switch fun.Sel.Name {
		case "Run":
			break // Ok
		case "RunParallel":
			// TODO
			t.taint("RunParallel is not supported")
			return analysisTainted
		default:
			// Analyze the call itself. Skip call.Fun.
			cur, _ := cur.ChildAt(edge.ExprStmt_X, -1).FirstChild()
			return x.checkRest(t, cur, analysisOk)
		}

		// If tt belongs to a parent test, the caller is using the wrong TB (or
		// doing something weird), in which case: taint the parent TB and move
		// on.
		if tt != t {
			tt.taint("called parent TB within Run call")
			return analysisTainted
		}

		// Sanity check - if there aren't two args, something weird is
		// happening.
		if len(call.Args) != 2 {
			t.taint("invalid call (wrong number of args)")
			return analysisTainted
		}

		// Now we know we have a Run call for the current TB.

		sig, ok := x.TypesInfo.TypeOf(call.Args[1]).(*types.Signature)
		if !ok {
			t.taint("invalid callback (not a function?)")
			return analysisTainted
		}
		if kind, ok := testKind(sig); !ok || kind != t.kind {
			t.taint("invalid callback: wrong signature")
			return analysisTainted
		}

		// TODO: Handle non-strings
		name := x.TypesInfo.Types[call.Args[0]].Value // may be zero
		if name == nil || name.Kind() != constant.String {
			t.taint("cannot determine subtest name")
			return analysisTainted
		}

		// TODO: Handle non-function literals
		var typ *ast.FuncType
		var body inspector.Cursor
		if lit, ok := call.Args[1].(*ast.FuncLit); ok {
			typ = lit.Type
			body = cur.ChildAt(edge.ExprStmt_X, -1).ChildAt(edge.CallExpr_Args, 1).ChildAt(edge.FuncLit_Body, -1)
		}

		tt = x.captureTest(constant.StringVal(name), call, t.kind, typ, body)
		tt.parent = t
		t.children = append(t.children, tt)
		return analysisOk
	}

	return x.checkForTaints(t, cur)
}

func (x *Context) checkRest(t *Test, cur inspector.Cursor, r analysisResult) analysisResult {
	var ok bool
	for {
		cur, ok = cur.NextSibling()
		if !ok {
			return r
		}
		r = max(r, x.checkForTaints(t, cur))
	}
}

func (x *Context) checkForTaints(t *Test, cur inspector.Cursor) analysisResult {
	// TODO: Be less conservative. If the TB is passed to a call, and that
	// assignment (to the call parameter) narrows the type definition to
	// something that doesn't have a Run method, we'll assume it's safe (which
	// it will be unless someone asserts back to *T or *B).
	//
	// Doing this for dynamic calls (e.g. interface methods, func values) is
	// functionally impossible, and doing it for external static calls requires
	// analyzing those packages, but we *can* do it for local static calls
	// without too much trouble.

	// Are there any references to the TB?
	for cur := range cur.Preorder((*ast.Ident)(nil)) {
		// Is it a var?
		v, ok := x.TypesInfo.Uses[cur.Node().(*ast.Ident)].(*types.Var)
		if !ok {
			continue
		}

		// Is it a TB?
		tt := t.findForVar(v)
		if tt == nil {
			continue
		}

		// We found a non-call reference to one of the test's TB, so we need to
		// invalidate it.
		tt.taint("non-call reference")
	}

	if t.isTainted() {
		return analysisTainted
	}
	return analysisOk
}

func (x *Context) reportTest(t *Test, prefix string) {
	fullName := prefix + t.name

	// Report the test.
	x.Reportf(t.at.Pos(), "Found: %s", fullName)

	if t.isTainted() {
		x.Reportf(t.at.Pos(), "Tainted (can't report children): %s", fullName)
		return
	}

	// Exclude children if there are any name collisions.
	count := map[string]int{}
	for _, tt := range t.children {
		count[tt.name]++
	}

	prefix = fullName + "/"
	for _, tt := range t.children {
		if count[tt.name] > 1 {
			continue
		}
		x.reportTest(tt, prefix)
	}
}

func (t *Test) taint(format string, args ...any) {
	t.tainted = append(t.tainted, fmt.Errorf(format, args...))
}

func (t *Test) isTainted() bool {
	if len(t.tainted) > 0 {
		return true
	}
	return t.parent != nil && t.parent.isTainted()
}

func (t *Test) findForVar(v *types.Var) *Test {
	if t.tb == v {
		return t
	}
	if t.parent == nil {
		return nil
	}
	return t.parent.findForVar(v)
}
