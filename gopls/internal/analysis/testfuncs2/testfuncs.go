package testfuncs

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"iter"
	"maps"
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

	// Attempt to report tests even if there are errors (to provide a better
	// user experience). The analyzer must be conservative - errors must not
	// trigger false positives.
	RunDespiteErrors: true,
}

type (
	Context struct {
		*analysis.Pass
		Inspect *inspector.Inspector
		Refs    map[*types.Var][]inspector.Cursor
		Decls   map[types.Object]inspector.Cursor
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

	Result struct {
		Name    string // name of the test
		Tainted string `json:",omitempty"` // reason why subtests could not be reported
	}
)

const (
	analysisOk analysisResult = iota
	analysisTainted
)

func run(pass *analysis.Pass) (any, error) {
	x := &Context{
		Pass:    pass,
		Inspect: pass.ResultOf[inspect.Analyzer].(*inspector.Inspector),
		Refs:    map[*types.Var][]inspector.Cursor{},
		Decls:   map[types.Object]inspector.Cursor{},
	}

	// Capture references to TBs.
	for cur := range x.Inspect.Root().Preorder((*ast.Ident)(nil)) {
		v, ok := x.TypesInfo.Uses[cur.Node().(*ast.Ident)].(*types.Var)
		if !ok || v.Kind() != types.ParamVar {
			continue
		} else if _, ok := tbKind(v.Type()); !ok {
			continue
		}
		x.Refs[v] = append(x.Refs[v], cur)
	}

	// Capture declarations.
	for cur := range x.Inspect.Root().Preorder((*ast.FuncDecl)(nil)) {
		fn, ok := x.TypesInfo.Defs[cur.Node().(*ast.FuncDecl).Name].(*types.Func)
		if !ok {
			continue
		}
		x.Decls[fn] = cur
	}

	seen := map[string]int{}
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
			t := x.captureTest(decl.Name.Name, decl, kind, decl.Type, body, nil)
			x.reportTest(t, "", seen)
		}
	}

	return nil, nil
}

func (x *Context) captureTest(name string, at ast.Node, kind *types.TypeName, typ *ast.FuncType, body inspector.Cursor, env map[*types.Var]value) *Test {
	// Don't recurse if we don't have a function type or body, or if this is an
	// example (kind == nil).
	t := &Test{name: rewrite(name), kind: kind, at: at}
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

	// Don't bother checking badly-formed tests. We could capture subtests
	// created prior to the invalidating statement, but that would make the
	// analysis significantly more complex.
	if !x.isWellFormed(t, body) {
		t.taint("TB escapes the test")
		return t
	}

	// Check for subtests.
	x.analyzeTest(t, body, env)
	return t
}

func (x *Context) analyzeTest(t *Test, cur inspector.Cursor, env map[*types.Var]value) analysisResult {
	switch node := cur.Node().(type) {
	case *ast.BlockStmt:
		return x.analyzeChildren(t, cur, env, edge.BlockStmt_List, len(node.List))

	case *ast.AssignStmt:
		// Check for `ok := t.Run(...)`.
		return x.analyzeChildren(t, cur, env, edge.AssignStmt_Rhs, len(node.Rhs))

	case *ast.DeclStmt:
		return x.analyzeTest(t, cur.ChildAt(edge.DeclStmt_Decl, -1), env)

	case *ast.GenDecl:
		return x.analyzeChildren(t, cur, env, edge.GenDecl_Specs, len(node.Specs))

	case *ast.ValueSpec:
		// Check for `var ok = t.Run(...)`.
		return x.analyzeChildren(t, cur, env, edge.ValueSpec_Values, len(node.Values))

	case *ast.TypeSpec, *ast.ArrayType, *ast.StructType, *ast.FuncType, *ast.InterfaceType, *ast.MapType, *ast.ChanType:
		// Don't care
		return analysisOk

	case *ast.RangeStmt:
		v, err := evaluateAs[seqValue](x, node.X, cur.ChildAt(edge.RangeStmt_X, -1), env)
		if err != nil {
			break
		}

		var K, V *types.Var
		if ident, ok := node.Key.(*ast.Ident); ok {
			K = x.TypesInfo.ObjectOf(ident).(*types.Var)
		}
		if ident, ok := node.Value.(*ast.Ident); ok {
			V = x.TypesInfo.ObjectOf(ident).(*types.Var)
		}

		// This re-analyzes the loop body on every iteration, which is arguably
		// wasted work. Separating analysis from emission would allow us to
		// analyze once and emit many times, but that requires deferring
		// evaluation of the name expression until emission.

		env := maps.Clone(env)
		if env == nil {
			env = make(map[*types.Var]value)
		}
		for k, v := range v.All() {
			if K != nil {
				env[K] = k
			}
			if V != nil {
				env[V] = v
			}
			switch x.analyzeTest(t, cur.ChildAt(edge.RangeStmt_Body, -1), env) {
			case analysisTainted:
				return analysisTainted
			}
		}
		if t.isTainted() {
			return analysisTainted
		}
		return analysisOk

	case *ast.ExprStmt:
		return x.analyzeTest(t, cur.ChildAt(edge.ExprStmt_X, -1), env)

	case *ast.CallExpr:
		// If this is not a TB method call, fall through. Calls on other TBs
		// will already have been caught by isWellFormed.
		fun, ok := node.Fun.(*ast.SelectorExpr)
		if !ok {
			return x.analyzeAllChildren(t, cur, env)
		}
		recv, ok := fun.X.(*ast.Ident)
		if !ok {
			return x.analyzeAllChildren(t, cur, env)
		}
		v, ok := x.TypesInfo.Uses[recv].(*types.Var)
		if !ok || v != t.tb {
			return x.analyzeAllChildren(t, cur, env)
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
			// Recurse in case someone does something insane like
			// `t.Log(t.Run(...))`.
			return x.analyzeChildren(t, cur, env, edge.CallExpr_Args, len(node.Args))
		}

		// Sanity check - if there aren't two args, something weird is
		// happening.
		if len(node.Args) != 2 {
			t.taint("invalid call (wrong number of args)")
			return analysisTainted
		}

		// Now we know we have a Run call for the current TB.

		sig, ok := x.TypesInfo.TypeOf(node.Args[1]).(*types.Signature)
		if !ok {
			t.taint("invalid callback (not a function?)")
			return analysisTainted
		}
		if kind, ok := testKind(sig); !ok || kind != t.kind {
			t.taint("invalid callback: wrong signature")
			return analysisTainted
		}

		name, err := evaluateAs[constValue](x, node.Args[0], cur.ChildAt(edge.CallExpr_Args, 0), env)
		if err != nil {
			t.taint("cannot determine subtest name: %v", err)
			return analysisTainted
		} else if name.Kind() != constant.String {
			t.taint("cannot determine subtest name: want %v, got %v", constant.String, name.Kind())
			return analysisTainted
		}

		callback, err := evaluateAs[funcValue](x, node.Args[1], cur.ChildAt(edge.CallExpr_Args, 1), env)
		if err != nil {
			err = fmt.Errorf("cannot determine callback: %v", err)
		}

		tt := x.captureTest(constant.StringVal(name.Value), node, t.kind, callback.typ, callback.body, env)
		tt.parent = t
		t.children = append(t.children, tt)
		if err != nil {
			tt.tainted = append(tt.tainted, err)
		}
		return analysisOk

	case ast.Expr:
		return x.analyzeAllChildren(t, cur, env)
	}

	// TODO: Don't suppress children.
	t.taint("unmodeled statement %T", cur.Node())
	return analysisTainted
}

func (x *Context) analyzeChildren(t *Test, cur inspector.Cursor, env map[*types.Var]value, edge edge.Kind, n int) analysisResult {
	for i := range n {
		switch x.analyzeTest(t, cur.ChildAt(edge, i), env) {
		case analysisTainted:
			return analysisTainted
		}
	}
	if t.isTainted() {
		return analysisTainted
	}
	return analysisOk
}

func (x *Context) analyzeAllChildren(t *Test, cur inspector.Cursor, env map[*types.Var]value) analysisResult {
	for cur := range cur.Children() {
		switch x.analyzeTest(t, cur, env) {
		case analysisTainted:
			return analysisTainted
		}
	}
	if t.isTainted() {
		return analysisTainted
	}
	return analysisOk
}

// isWellFormed verifies that the test's TB:
//
//   - Is not referenced by any subtest.
//   - Is not passed to a function or stored.
//   - Unless type of the function parameter or storage location excludes the Run method.
//
// This explicitly does not account for `tb.(*testing.T)` assertions.
func (x *Context) isWellFormed(t *Test, cur inspector.Cursor) bool {
	// Find the enclosing function (there must be one).
	fn1, _ := first(cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)))

	// Are there any references to the TB?
	for _, cur := range x.Refs[t.tb] {
		// Is it within the same function?
		fn2, _ := first(cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)))
		if fn1 != fn2 {
			return false
		}

		// Ignore TB method calls.
		if cur.ParentEdgeKind() == edge.SelectorExpr_X &&
			cur.Parent().ParentEdgeKind() == edge.CallExpr_Fun {
			continue
		}

		// TODO: allow safe uses.
		return false
	}

	return true
}

func (x *Context) reportTest(t *Test, prefix string, seen map[string]int) {
	var r Result
	r.Name = uniqueName(prefix, t.name, seen)
	for i, err := range t.tainted {
		if i > 0 {
			r.Tainted += "; "
		}
		r.Tainted += err.Error()
	}

	x.Report(analysis.Diagnostic{
		Pos:     t.at.Pos(),
		End:     t.at.End(),
		Message: r.String(),
	})
	if t.isTainted() {
		return
	}

	prefix = r.Name + "/"
	for _, tt := range t.children {
		x.reportTest(tt, prefix, seen)
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

func (r *Result) String() string {
	b, err := json.Marshal(r)
	if err != nil {
		// Results is dead simple, this should never happen.
		panic(fmt.Errorf("cannot encode testfuncs result: %v", err))
	}
	return string(b)
}

func first[V any](seq iter.Seq[V]) (V, bool) {
	for v := range seq {
		return v, true
	}

	var z V
	return z, false
}
