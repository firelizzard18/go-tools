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
	"slices"
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
		indices
		Inspect *inspector.Inspector
	}

	analysisContext struct {
		*Context
		Test *Test
		TB   *testParam
		Seen []*testFunc
	}

	Test struct {
		parent   *Test
		kind     *types.TypeName
		name     string
		at       ast.Node
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
	}
	x.buildIndices()

	seen := map[string]int{}
	for cur := range x.Inspect.Root().Children() {
		// We only care about test files
		if !strings.HasSuffix(x.Fset.Position(cur.Node().Pos()).Filename, "_test.go") {
			continue
		}

		for cur := range cur.Preorder((*ast.FuncDecl)(nil)) {
			fn, ok := x.TypesInfo.Defs[cur.Node().(*ast.FuncDecl).Name].(*types.Func)
			if !ok || !fn.Exported() {
				continue
			}

			// error.Error has empty Position, PkgPath, and ObjectPath.
			if fn.Pkg() == nil {
				continue
			}

			kind, ok := isTestOrExample(fn)
			if !ok {
				continue
			}

			// Examples are special.
			t := &Test{name: rewrite(fn.Name()), kind: kind, at: cur.Node()}
			if kind == nil {
				x.reportTest(t, "", seen)
				continue
			}

			meta, ok := x.FuncDecls[fn]
			if !ok {
				continue
			}

			// `isTestOrExample` passed, so meta.Params __must__ have exactly
			// one param.
			tb, _ := first(maps.Values(meta.Params))
			analysisContext{x, t, tb, nil}.analyzeFunc(meta, nil)
			x.reportTest(t, "", seen)
		}
	}

	return nil, nil
}

func (x analysisContext) child(name string, at ast.Node, tb *testParam) analysisContext {
	x.Test = &Test{
		parent: x.Test,
		name:   rewrite(name),
		kind:   x.Test.kind,
		at:     at,
	}
	x.TB = tb
	return x
}

func (x analysisContext) analyzeFunc(fn *testFunc, env map[*types.Var]value) analysisResult {
	if slices.Contains(x.Seen, fn) {
		x.Test.taint("recursive call")
		return analysisTainted
	}

	// Don't check tests where the TB escapes (to a closure, field, variable,
	// etc).
	if x.TB.Escapes {
		x.Test.taint("TB escapes the test")
		return analysisTainted
	}

	// x is pass-by-value so the caller won't see this.
	x.Seen = append(x.Seen, fn)
	return x.analyze(fn.Body, env)
}

func (x analysisContext) analyze(cur inspector.Cursor, env map[*types.Var]value) analysisResult {
	var ok analysisResult
	cur.Inspect(nil, func(cur inspector.Cursor) (descend bool) {
		switch node := cur.Node().(type) {
		case *ast.RangeStmt:
			v, err := evaluateAs[seqValue](x.Context, node.X, cur.ChildAt(edge.RangeStmt_X, -1), env)
			if err != nil {
				break
			}

			ok = x.analyzeRange(cur, v, env)
			return false

		case *ast.Ident:
			ok = x.analyzeIdent(cur, env)
			return false

		case *ast.TypeSpec, *ast.ArrayType, *ast.StructType, *ast.FuncType, *ast.InterfaceType, *ast.MapType, *ast.ChanType:
			// Ignore types.
			return false

		case *ast.FuncLit:
			// Don't descend into closures.
			return false

		case *ast.DeclStmt, *ast.GenDecl, *ast.ValueSpec, *ast.AssignStmt:
			// Check for `ok := t.Run(...)`.
			return true

		case *ast.BlockStmt, *ast.ExprStmt, ast.Expr:
			// Recurse.
			return true
		}

		ok = analysisTainted
		x.Test.taint("unmodeled statement %T", cur.Node())
		return false
	})
	return ok
}

func (x analysisContext) analyzeRange(cur inspector.Cursor, v seqValue, env map[*types.Var]value) analysisResult {
	var K, V *types.Var
	node := cur.Node().(*ast.RangeStmt)
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

	env = maps.Clone(env)
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
		switch x.analyze(cur.ChildAt(edge.RangeStmt_Body, -1), env) {
		case analysisTainted:
			return analysisTainted
		}
	}
	if x.Test.isTainted() {
		return analysisTainted
	}
	return analysisOk
}

func (x analysisContext) analyzeIdent(cur inspector.Cursor, env map[*types.Var]value) analysisResult {
	switch x.TB.Refs[cur] {
	case safeTBRef:
		return analysisOk

	case tbAsCallArg:
		// Attempt to resolve the function.
		i := cur.ParentEdgeIndex()
		call := cur.Parent().Node().(*ast.CallExpr)
		fn, err := evaluateAs[*testFunc](x.Context, call.Fun, cur.Parent(), env)
		if err != nil {
			x.Test.taint("cannot resolve function call")
			return analysisTainted
		} else if i >= fn.Type.Params().Len()-1 && fn.Type.Variadic() {
			x.Test.taint("cannot trace TB through variadic call")
			return analysisTainted
		} else if i >= fn.Type.Params().Len() {
			x.Test.taint("invalid number of parameters")
			return analysisTainted
		}

		// x is pass-by-value so the caller won't see this.
		x.TB = fn.Params[fn.Type.Params().At(i)]
		return x.analyzeFunc(fn, env)

	case tbRunCall:
		cur = cur.Parent()
		switch cur.Node().(*ast.SelectorExpr).Sel.Name {
		case "RunParallel":
			// TODO
			x.Test.taint("RunParallel is not supported")
			return analysisTainted
		}

		// Must be Run.
		cur = cur.Parent()
		call := cur.Node().(*ast.CallExpr)
		if len(call.Args) != 2 {
			x.Test.taint("invalid call (wrong number of args)")
			return analysisTainted
		}

		name, err := evaluateAs[constValue](x.Context, call.Args[0], cur.ChildAt(edge.CallExpr_Args, 0), env)
		if err != nil {
			x.Test.taint("cannot determine subtest name: %v", err)
			return analysisTainted
		} else if name.Kind() != constant.String {
			x.Test.taint("cannot determine subtest name: want %v, got %v", constant.String, name.Kind())
			return analysisTainted
		}

		// Can we resolve the callback and is it the correct kind?
		callback, err := evaluateAs[*testFunc](x.Context, call.Args[1], cur.ChildAt(edge.CallExpr_Args, 1), env)
		var tb *testParam
		if err != nil {
			err = fmt.Errorf("cannot determine callback: %v", err)
		} else if kind, ok := testKind(callback.Type); !ok || kind != x.Test.kind {
			err = fmt.Errorf("invalid callback: wrong signature")
		} else {
			// `testKind` passed, so callback.Params __must__ have exactly one
			// param.
			tb, _ = first(maps.Values(callback.Params))
		}

		y := x.child(constant.StringVal(name.Value), call, tb)
		if err != nil {
			y.Test.tainted = append(y.Test.tainted, err)
		} else {
			// TODO: Don't append if analysis fails?
			y.analyzeFunc(callback, env)
		}

		x.Test.children = append(x.Test.children, y.Test)
		return analysisOk
	}

	x.Test.taint("unsafe reference to TB")
	return analysisTainted // TODO: ???
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
