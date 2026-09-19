package testfuncs

import (
	_ "embed"
	"encoding/json"
	"errors"
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
		errors   []*Error
		children []*Test
	}

	Result struct {
		Name   string   ``                  // name of the test
		Errors []*Error `json:",omitempty"` // reason why subtests could not be reported
	}

	ErrorKind int

	Error struct {
		kind  ErrorKind
		inner error
	}
)

const (
	errUnknown   ErrorKind = iota // An unknown error occurred.
	errUnmodeled                  // An unmodeled statement or expression.
	errInvalid                    // An invalid expression (one that fails typechecking).
	errRecursed                   // A recursive call.
	errTBEscapes                  // The TB parameter escaped.
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

func (x analysisContext) child(name string, at ast.Node) analysisContext {
	x.Test = &Test{
		parent: x.Test,
		name:   rewrite(name),
		kind:   x.Test.kind,
		at:     at,
	}
	return x
}

func (x analysisContext) analyzeFunc(fn *testFunc, env map[*types.Var]value) bool {
	if slices.Contains(x.Seen, fn) {
		x.Test.error(errRecursed)
		return false
	}

	// Don't check tests where the TB escapes (to a closure, field, variable,
	// etc).
	if x.TB.Escapes {
		x.Test.error(errTBEscapes)
		return false
	}

	// x is pass-by-value so the caller won't see this.
	x.Seen = append(x.Seen, fn)
	return x.analyze(fn.Body, env)
}

func (x analysisContext) analyze(cur inspector.Cursor, env map[*types.Var]value) bool {
	ok := true
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

		x.Test.errorf(errUnmodeled, "unmodeled statement %T", cur.Node())
		ok = false
		return false
	})
	return ok
}

func (x analysisContext) analyzeRange(cur inspector.Cursor, v seqValue, env map[*types.Var]value) bool {
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
	i := len(x.Test.children)
	for k, v := range v.All() {
		if K != nil {
			env[K] = k
		}
		if V != nil {
			env[V] = v
		}
		if !x.analyze(cur.ChildAt(edge.RangeStmt_Body, -1), env) {
			// If the analysis halts, remove children to avoid
			// first-iteration-only subtests.
			x.Test.children = x.Test.children[:i]
			return false
		}
	}
	return true
}

func (x analysisContext) analyzeIdent(cur inspector.Cursor, env map[*types.Var]value) bool {
	switch x.TB.Refs[cur] {
	default:
		return true

	case unsafeTBRef:
		x.Test.errorf(errTBEscapes, "unsafe reference to TB")
		return false

	case tbAsCallArg:
		// TB is being passed to a function. Failure to resolve must be treated
		// as a TB escape.
		i := cur.ParentEdgeIndex()
		call := cur.Parent().Node().(*ast.CallExpr)
		fn, err := evaluateAs[*testFunc](x.Context, call.Fun, cur.Parent(), env)
		if err != nil {
			x.Test.errorf(errTBEscapes, "cannot resolve function call")
			return true
		} else if i >= fn.Type.Params().Len()-1 && fn.Type.Variadic() {
			x.Test.errorf(errTBEscapes, "cannot trace TB through variadic call")
			return true
		} else if i >= fn.Type.Params().Len() {
			x.Test.errorf(errTBEscapes, "invalid number of parameters")
			return true
		}

		// x is pass-by-value so the caller won't see this.
		x.TB = fn.Params[fn.Type.Params().At(i)]
		return x.analyzeFunc(fn, env)

	case tbRunCall:
		cur = cur.Parent()
		switch cur.Node().(*ast.SelectorExpr).Sel.Name {
		case "RunParallel":
			// TODO
			x.Test.errorf(errUnmodeled, "RunParallel is not supported")
			return false
		}

		// Must be Run.
		cur = cur.Parent()
		call := cur.Node().(*ast.CallExpr)
		if len(call.Args) != 2 {
			x.Test.errorf(errInvalid, "invalid call (wrong number of args)")
			return false
		}

		// Determine the name. If we can't, we must stop analysis because we
		// can't deduplicate subsequent subtest names correctly.
		name, err := evaluateAs[constValue](x.Context, call.Args[0], cur.ChildAt(edge.CallExpr_Args, 0), env)
		if e := new(Error); errors.As(err, &e) {
			x.Test.errors = append(x.Test.errors, e)
			return false
		} else if err != nil {
			x.Test.errorf(errUnknown, "cannot determine subtest name: %v", err)
			return false
		} else if name.Kind() != constant.String {
			x.Test.errorf(errUnmodeled, "cannot determine subtest name: want %v, got %v", constant.String, name.Kind())
			return false
		}

		// We know the name so we can create a child test.
		y := x.child(constant.StringVal(name.Value), call)
		x.Test.children = append(x.Test.children, y.Test)

		// Can we resolve the callback and is it the correct kind?
		callback, err := evaluateAs[*testFunc](x.Context, call.Args[1], cur.ChildAt(edge.CallExpr_Args, 1), env)
		if e := new(Error); errors.As(err, &e) {
			y.Test.errors = append(y.Test.errors, e)
		} else if err != nil {
			y.Test.errorf(errUnknown, "cannot determine callback: %v", err)
		} else if kind, ok := testKind(callback.Type); !ok || kind != x.Test.kind {
			y.Test.errorf(errInvalid, "invalid callback: wrong signature")
		} else {
			// `testKind` passed, so callback.Params __must__ have exactly one
			// param.
			y.TB, _ = first(maps.Values(callback.Params))
		}

		// Analyze the child call. As long as the parent's TB doesn't escape
		// (which is checked earlier), nothing that happens in the child affects
		// the validity of the parent.
		if len(y.Test.errors) == 0 {
			y.analyzeFunc(callback, env)
		}
		return true
	}
}

func (x *Context) reportTest(t *Test, prefix string, seen map[string]int) {
	var r Result
	r.Name = uniqueName(prefix, t.name, seen)
	r.Errors = t.errors

	x.Report(analysis.Diagnostic{
		Pos:     t.at.Pos(),
		End:     t.at.End(),
		Message: r.String(),
	})

	prefix = r.Name + "/"
	for _, tt := range t.children {
		x.reportTest(tt, prefix, seen)
	}
}

func (e *Error) Error() string {
	return ""
}

func (t *Test) error(kind ErrorKind) {
	t.errors = append(t.errors, &Error{kind: kind})
}

func (t *Test) errorf(kind ErrorKind, format string, args ...any) {
	t.errors = append(t.errors, &Error{kind: kind, inner: fmt.Errorf(format, args...)})
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
