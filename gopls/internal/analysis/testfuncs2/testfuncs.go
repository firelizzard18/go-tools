package testfuncs

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
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
		Name   string
		Error  errorKind `json:",omitempty"`
		Reason string    `json:",omitempty"`
	}

	errorKind int

	Error struct {
		kind  errorKind
		at    token.Pos
		inner error
	}
)

const (
	errUnknown    errorKind = iota // An unknown error occurred.
	errUnmodeled                   // An unmodeled statement or expression.
	errUnresolved                  // An ident that could not be resolved.
	errInvalid                     // An invalid expression (one that fails typechecking).
	errRecursed                    // A recursive call.
	errTBEscapes                   // The TB parameter escaped.
)

var (
	errNames = [...]string{
		errUnknown:    "unknown",
		errUnmodeled:  "unmodeled",
		errUnresolved: "unresolved",
		errInvalid:    "invalid",
		errRecursed:   "recursed",
		errTBEscapes:  "escapes",
	}
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
		x.Test.error(fn.Body.Node(), errRecursed)
		return false
	}

	// Don't check tests where the TB escapes (to a closure, field, variable,
	// etc).
	if x.TB.Escapes {
		x.Test.error(fn.Body.Node(), errTBEscapes)
		return false
	}

	// x is pass-by-value so the caller won't see this.
	x.Seen = append(x.Seen, fn)
	return x.analyze(fn.Body, env, nil)
}

func (x analysisContext) analyze(cur inspector.Cursor, env map[*types.Var]value, at ast.Node) bool {
	ok := true
	cur.Inspect(nil, func(cur inspector.Cursor) (descend bool) {
		if !ok {
			return false
		}
		switch cur.Node().(type) {
		case *ast.Ident:
			ok = x.analyzeIdent(cur, env, at)
			return false

		case *ast.RangeStmt:
			ok = x.analyzeRange(cur, env, at)
			return false

		case *ast.TypeSpec, *ast.ArrayType, *ast.StructType, *ast.FuncType, *ast.InterfaceType, *ast.MapType, *ast.ChanType,
			*ast.FuncLit, *ast.EmptyStmt, *ast.CommentGroup, *ast.Comment:
			// Don't care. Closures (FuncLits) specifically are checked via
			// other mechanisms.
			return false

		case *ast.BlockStmt, *ast.ExprStmt, ast.Expr,
			*ast.LabeledStmt, *ast.IncDecStmt, *ast.ReturnStmt,
			*ast.AssignStmt, *ast.ValueSpec, *ast.GenDecl, *ast.DeclStmt:
			// Recurse. We recurse on ValueSpec/AssignStmt to handle `ok :=
			// t.Run(...)`.
			return true
		}

		x.Test.errorf(cur.Node(), errUnmodeled, "unmodeled statement %T", cur.Node())
		ok = false
		return false
	})
	return ok
}

func (x analysisContext) analyzeIdent(cur inspector.Cursor, env map[*types.Var]value, at ast.Node) bool {
	// Is this our TB?
	v, ok := x.TypesInfo.Uses[cur.Node().(*ast.Ident)].(*types.Var)
	if !ok || x.TB.Var != v {
		return true
	}

	switch cur.ParentEdgeKind() {
	case edge.CallExpr_Args:
		// Passed as an argument to a call.
		//
		// If the parameter V is being passed to is runnable (or if we
		// can't determine the function signature), record the call
		call := cur.Parent().Node().(*ast.CallExpr)
		typ, ok := x.TypesInfo.TypeOf(call.Fun).(*types.Signature)
		if !ok {
			x.Test.errorf(call.Fun, errTBEscapes, "TB passed to call: cannot determine function signature")
			return false
		}

		// If the parameter isn't runnable, we don't care about it.
		i := cur.ParentEdgeIndex()
		if typ.Variadic() && i >= typ.Params().Len()-1 {
			x.Test.errorf(cur.Node(), errTBEscapes, "TB passed to call as variadic argument")
			return false
		}
		if i >= typ.Params().Len() {
			x.Test.errorf(cur.Node(), errTBEscapes, "TB passed to call as invalid argument")
			return false
		}
		if !isRunnableParam(typ, i) {
			return true
		}

		fn, err := evaluateAs[*testFunc](x.Context, call.Fun, cur.Parent(), env)
		if err != nil {
			x.Test.errorf(call.Fun, errTBEscapes, "TB passed to call: cannot resolve function")
			return false
		}

		// x is pass-by-value so the caller won't see this.
		x.TB = fn.Params[fn.Type.Params().At(i)]
		if x.TB == nil {
			x.Test.errorf(cur.Node(), errTBEscapes, "TB passed to call: cannot resolve TB parameter")
			return false
		}

		return x.analyzeFunc(fn, env)

	case edge.SelectorExpr_X:
		// If the parent is not a CallExpr, something weird is
		// happening.
		cur = cur.Parent()
		if cur.ParentEdgeKind() != edge.CallExpr_Fun {
			x.Test.errorf(cur.Parent().Node(), errTBEscapes, "unsafe reference to TB")
			return false
		}

		// Accessing a method.
		//
		// If the method isn't Run, we don't care. Weird nested calls (e.g.
		// `t.Log(t.Run(...))`) will be caught by the main Inspect loop.
		if cur.Node().(*ast.SelectorExpr).Sel.Name != "Run" {
			return true
		}

		cur = cur.Parent()
		call := cur.Node().(*ast.CallExpr)
		if len(call.Args) != 2 {
			x.Test.errorf(call, errInvalid, "invalid call (wrong number of args)")
			return false
		}

		// Determine the name. If we can't, we must stop analysis because we
		// can't deduplicate subsequent subtest names correctly.
		name, err := evaluateAs[constValue](x.Context, call.Args[0], cur.ChildAt(edge.CallExpr_Args, 0), env)
		if e := new(Error); errors.As(err, &e) {
			x.Test.errors = append(x.Test.errors, e)
			return false
		} else if err != nil {
			x.Test.errorf(call.Args[0], errUnknown, "cannot determine subtest name: %v", err)
			return false
		} else if name.Kind() != constant.String {
			x.Test.errorf(call.Args[0], errUnmodeled, "cannot determine subtest name: want %v, got %v", constant.String, name.Kind())
			return false
		}

		// We know the name so we can create a child test. Prefer `at` for
		// reporting the position so that the location of a table driven test is
		// reported as the table entry's location.
		y := x.child(constant.StringVal(name.Value), cmp.Or[ast.Node](at, call))
		x.Test.children = append(x.Test.children, y.Test)

		// Can we resolve the callback and is it the correct kind?
		callback, err := evaluateAs[*testFunc](x.Context, call.Args[1], cur.ChildAt(edge.CallExpr_Args, 1), env)
		if e := new(Error); errors.As(err, &e) {
			y.Test.errors = append(y.Test.errors, e)
		} else if err != nil {
			y.Test.errorf(call.Args[1], errUnknown, "cannot determine callback: %v", err)
		} else if kind, ok := testKind(callback.Type); !ok || kind != x.Test.kind {
			y.Test.errorf(call.Args[1], errInvalid, "invalid callback: wrong signature")
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

	case edge.AssignStmt_Rhs:
		// Already folded into Escapes, so ignore.
		return true

	default:
		// Consider anything else to be unsafe.
		x.Test.errorf(cur.Node(), errTBEscapes, "unsafe reference to TB")
		return false
	}
}

func (x analysisContext) analyzeRange(cur inspector.Cursor, env map[*types.Var]value, _ ast.Node) bool {
	node := cur.Node().(*ast.RangeStmt)
	v, err := evaluateAs[seqValue](x.Context, node.X, cur.ChildAt(edge.RangeStmt_X, -1), env)
	if e := new(Error); errors.As(err, &e) {
		x.Test.errors = append(x.Test.errors, e)
		return false
	} else if err != nil {
		x.Test.errorf(cur.Node(), errUnknown, "cannot resolve range var: %w", err)
		return false
	}

	var K, V *types.Var
	if ident, ok := node.Key.(*ast.Ident); ok && ident.Name != "_" {
		K = x.TypesInfo.ObjectOf(ident).(*types.Var)
	}
	if ident, ok := node.Value.(*ast.Ident); ok && ident.Name != "_" {
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
	for kv := range v.All() {
		if K != nil {
			env[K] = kv.key
		}
		if V != nil {
			env[V] = kv.value
		}
		if !x.analyze(cur.ChildAt(edge.RangeStmt_Body, -1), env, kv.src) {
			// If the analysis halts, remove children to avoid
			// first-iteration-only subtests.
			x.Test.children = x.Test.children[:i]
			return false
		}
	}
	return true
}

func (x *Context) reportTest(t *Test, prefix string, seen map[string]int) {
	var r Result
	r.Name = uniqueName(prefix, t.name, seen)
	x.Report(analysis.Diagnostic{
		Pos:     t.at.Pos(),
		End:     t.at.End(),
		Message: r.String(),
	})

	for _, err := range t.errors {
		x.Report(analysis.Diagnostic{
			Pos: err.at,
			Message: Result{
				Name:   r.Name,
				Error:  err.kind,
				Reason: err.inner.Error(),
			}.String(),
		})

	}

	prefix = r.Name + "/"
	for _, tt := range t.children {
		x.reportTest(tt, prefix, seen)
	}
}

func (t *Test) error(at ast.Node, kind errorKind) {
	t.errors = append(t.errors, &Error{kind: kind, at: at.Pos()})
}

func (t *Test) errorf(at ast.Node, kind errorKind, format string, args ...any) {
	t.errors = append(t.errors, errorf(at, kind, format, args...))
}

func errorf(at ast.Node, kind errorKind, format string, args ...any) *Error {
	return &Error{kind: kind, at: at.Pos(), inner: fmt.Errorf(format, args...)}
}

func (r Result) String() string {
	b, err := json.Marshal(r)
	if err != nil {
		// Results is dead simple, this should never happen.
		panic(fmt.Errorf("cannot encode testfuncs result: %v", err))
	}
	return string(b)
}

func (e *Error) Error() string {
	return e.inner.Error()
}

func (k errorKind) String() string {
	return errNames[k]
}

func (e errorKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.String())
}

func (e *errorKind) UnmarshalJSON(b []byte) error {
	var s string
	err := json.Unmarshal(b, &s)
	if err != nil {
		return err
	}
	i := slices.Index(errNames[:], s)
	if i < 0 {
		return fmt.Errorf("%q is not a valid error kind", s)
	}
	*e = errorKind(i)
	return nil
}

func first[V any](seq iter.Seq[V]) (V, bool) {
	for v := range seq {
		return v, true
	}

	var z V
	return z, false
}
