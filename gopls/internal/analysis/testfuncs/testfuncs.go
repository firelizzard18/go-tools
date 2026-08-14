// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testfuncs

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"iter"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/internal/analysis/analyzerutil"
)

//go:embed doc.go
var doc string

// mode selects how the analyzer resolves the value of a variable.
type mode int

const (
	// modeHybrid resolves variables using SSA, falling back to syntax.
	modeHybrid mode = iota

	// modeAST resolves variables using syntax and type information only. It
	// does not require (and therefore does not pay for) SSA construction.
	modeAST
)

// Analyzer enumerates tests and subtests, resolving variables with SSA.
var Analyzer = newAnalyzer("testfuncs", modeHybrid)

// ASTAnalyzer enumerates tests and subtests, resolving variables using syntax
// alone. It is otherwise identical to [Analyzer].
var ASTAnalyzer = newAnalyzer("testfuncsast", modeAST)

func newAnalyzer(name string, mode mode) *analysis.Analyzer {
	var debug, stats bool

	a := &analysis.Analyzer{
		Name:     name,
		Doc:      analyzerutil.MustExtractDoc(doc, "testfuncs"),
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		URL:      "https://pkg.go.dev/golang.org/x/tools/gopls/internal/analysis/testfuncs",
	}
	if mode == modeHybrid {
		a.Requires = append(a.Requires, buildssa.Analyzer)
	}
	// Not named -debug: the analysis drivers already define that flag.
	a.Flags.BoolVar(&debug, "explain", false,
		"report constructs the analyzer cannot interpret, as diagnostics")
	a.Flags.BoolVar(&stats, "stats", false,
		"report one summary record per test function, and nothing else")
	a.Run = func(pass *analysis.Pass) (any, error) {
		// -stats output is meant to be parsed, so it must be the only thing on
		// the wire. Rather than silently letting one flag win, refuse: a
		// statistics run that quietly dropped its explanations, or vice versa,
		// would be discovered only after the corpus had been processed.
		if debug && stats {
			return nil, fmt.Errorf("-explain and -stats are mutually exclusive")
		}
		return run(pass, mode, debug, stats)
	}
	return a
}

type (
	Context struct {
		*analysis.Pass
		Inspect *inspector.Inspector
		SSA     *buildssa.SSA // nil unless mode is modeHybrid
		mode    mode
		debug   bool
		stats   bool

		// record is the summary of the test function currently being walked,
		// and pending is the set of reasons observed since the last subtest
		// was successfully resolved. Both are nil/zero unless stats is set.
		record  *statsRecord
		pending reason

		// Names counts the subtest names reported under each parent, in order
		// to reproduce the testing package's "#NN" disambiguation.
		Names map[string]int

		// Values holds the current binding of each range variable. It is
		// keyed by object rather than by syntax so that every reference to the
		// variable sees the binding, and it is saved and restored around each
		// iteration by [TestRange.Eval] so that bindings do not leak.
		Values map[types.Object]Expression

		// rangeVars is the set of variables bound by a range statement.
		rangeVars map[types.Object]bool

		// bindings records every site at which a variable is assigned, and
		// refs counts every mention of it, across the whole package.
		bindings map[types.Object][]binding
		refs     map[types.Object]int
	}

	// Test is a discovered test or subtest. If ok is false the test exists but
	// its name could not be determined; see [Context.reportTests].
	Test struct {
		name string
		fn   *FuncExpr
		at   token.Pos
		ok   bool
	}

	TestExpr interface {
		Eval(*Context) iter.Seq[Test]
	}

	// TestDecl is a top-level test function.
	TestDecl ast.FuncDecl

	// TestCall is a call to testing.TB.Run.
	TestCall struct {
		name, fn Expression
		pos      token.Pos
	}

	// TestRange is a range statement whose body creates subtests.
	TestRange struct {
		key, val types.Object
		x        Expression
		children []TestExpr
	}

	// unresolvedTests marks a statement that creates an unknown number of
	// subtests with unknown names.
	unresolvedTests struct {
		pos token.Pos
		why reason
	}
)

func run(pass *analysis.Pass, mode mode, debug, stats bool) (any, error) {
	x := &Context{
		Pass:      pass,
		Inspect:   pass.ResultOf[inspect.Analyzer].(*inspector.Inspector),
		mode:      mode,
		debug:     debug,
		stats:     stats,
		Names:     map[string]int{},
		Values:    map[types.Object]Expression{},
		rangeVars: map[types.Object]bool{},
		bindings:  map[types.Object][]binding{},
		refs:      map[types.Object]int{},
	}
	if mode == modeHybrid {
		x.SSA = pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	}
	x.survey()

	// Find top-level tests. Each declaration is visited exactly once; the
	// test-file check is applied per declaration rather than per file, because
	// the inspector is package-wide.
	x.Inspect.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
		decl := n.(*ast.FuncDecl)
		if !strings.HasSuffix(x.Fset.Position(decl.Pos()).Filename, "_test.go") {
			return
		}

		obj, ok := x.TypesInfo.ObjectOf(decl.Name).(*types.Func)
		if !ok || !obj.Exported() {
			return
		}

		// error.Error has empty Position, PkgPath, and ObjectPath.
		if obj.Pkg() == nil {
			return
		}

		if !isTestOrExample(obj) {
			return
		}

		if x.stats {
			x.record = &statsRecord{
				name: decl.Name.Name,
				pos:  decl.Pos(),
				runs: x.countRuns(decl),
			}
			x.pending = 0
		}

		x.reportTests("", []TestExpr{(*TestDecl)(decl)})

		if x.stats {
			x.reportStats()
		}
	})
	return nil, nil
}

// survey collects the package-wide facts the resolvers need: which variables
// are bound by a range statement, where each variable is assigned, and how
// many times each is mentioned.
func (x *Context) survey() {
	nodes := []ast.Node{
		(*ast.RangeStmt)(nil),
		(*ast.AssignStmt)(nil),
		(*ast.ValueSpec)(nil),
		(*ast.Ident)(nil),
	}

	// note records that obj is assigned expr by stmt. expr is nil if the
	// assignment is not a simple one-to-one binding.
	note := func(name ast.Expr, expr ast.Expr, stmt ast.Node) {
		id, ok := name.(*ast.Ident)
		if !ok {
			return
		}
		obj := x.TypesInfo.ObjectOf(id)
		if obj == nil {
			return
		}
		x.bindings[obj] = append(x.bindings[obj], binding{expr, stmt})
	}

	x.Inspect.Preorder(nodes, func(n ast.Node) {
		switch n := n.(type) {
		case *ast.RangeStmt:
			// A range variable's value is known only while [TestRange.Eval]
			// is iterating, never from its declaration.
			if n.Tok != token.DEFINE {
				return
			}
			for _, e := range []ast.Expr{n.Key, n.Value} {
				if id, ok := e.(*ast.Ident); ok {
					if obj := x.TypesInfo.Defs[id]; obj != nil {
						x.rangeVars[obj] = true
					}
				}
			}

		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				if len(n.Lhs) == len(n.Rhs) {
					note(lhs, n.Rhs[i], n)
				} else {
					note(lhs, nil, n)
				}
			}

		case *ast.ValueSpec:
			for i, name := range n.Names {
				if i < len(n.Values) {
					note(name, n.Values[i], n)
				} else if len(n.Values) > 0 {
					note(name, nil, n)
				}
				// A ValueSpec with no values declares a zero value. It is not
				// recorded as a binding, so such a variable can only resolve
				// if it is assigned exactly once elsewhere.
			}

		case *ast.Ident:
			obj := x.TypesInfo.Defs[n]
			if obj == nil {
				obj = x.TypesInfo.Uses[n]
			}
			if obj != nil {
				x.refs[obj]++
			}
		}
	})
}

// reportTests reports the tests produced by exprs, which must be the complete
// set of subtests of a single parent.
//
// If any one of them cannot be resolved, none are reported. An unresolved
// sibling may consume a name that would otherwise have been unique, which
// would shift the "#NN" suffixes of the tests we can resolve. Reporting
// nothing is always acceptable; reporting a wrong name is not.
func (x *Context) reportTests(prefix string, exprs []TestExpr) {
	var tests []Test
	for _, expr := range exprs {
		for test := range expr.Eval(x) {
			if !test.ok {
				if x.debug {
					x.Reportf(test.at, "Unable to resolve all subtests of %q", prefix)
				}
				x.unresolved()
				return
			}
			// This test resolved, so whatever reasons were noted while
			// evaluating it did not prevent enumeration; discard them.
			x.pending = 0
			tests = append(tests, test)
		}
	}

	for _, test := range tests {
		fullName := x.uniqueName(prefix, test.name)
		if x.stats {
			// prefix is empty only for the top-level function itself, which
			// is not one of its own subtests.
			if prefix != "" {
				x.record.subtests++
			}
		} else {
			x.Reportf(test.at, "Found: %s", fullName)
		}

		if test.fn == nil {
			continue
		}
		x.reportTests(fullName+"/", slices.Collect(x.findSubTestsOf(test.fn.typ, test.fn.body)))
	}
}

func (x *Context) findSubTestsOf(typ *ast.FuncType, body *ast.BlockStmt) iter.Seq[TestExpr] {
	return func(yield func(TestExpr) bool) {
		// If the [testing.T] parameter is unnamed, the func cannot call
		// [testing.T.Run] and thus cannot create any subtests.
		if body == nil ||
			len(typ.Params.List) != 1 ||
			len(typ.Params.List[0].Names) == 0 {
			return
		}

		// This "can't fail" because testKind should guarantee that the function has
		// one parameter and the check above guarantees that parameter is named
		tb := x.TypesInfo.ObjectOf(typ.Params.List[0].Names[0])

		for expr := range x.findSubTests(tb, body) {
			if !yield(expr) {
				return
			}
		}
	}
}

func (x *Context) findSubTests(tb types.Object, stmt ast.Stmt) iter.Seq[TestExpr] {
	return func(yield func(TestExpr) bool) {
		// bail reports that stmt may create subtests we cannot enumerate. If
		// the statement provably contains no call to tb.Run it is simply
		// ignored, which keeps the common case (assertions, logging, control
		// flow) from poisoning every result.
		bail := func() {
			if x.callsRun(tb, stmt) {
				yield(&unresolvedTests{stmt.Pos(), x.bailReason(tb, stmt)})
			}
		}

		switch stmt := stmt.(type) {
		case *ast.ExprStmt:
			// An ExprStmt must be a call or a channel receive. So, CallExpr is the
			// only ExprStmt we care about.
			call, ok := stmt.X.(*ast.CallExpr)
			if !ok {
				return
			}

			// Recursing into arbitrary functions and methods is explicitly out of
			// scope, so all we care about here is `TB.Run` calls. Additionally, we only
			// care about calls where the receiver is the parent scope's `tb`.
			fun, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || fun.Sel.Name != "Run" {
				bail()
				return
			}
			recv, ok := fun.X.(*ast.Ident)
			if !ok || x.TypesInfo.ObjectOf(recv) != tb {
				bail()
				return
			}

			if len(call.Args) != 2 {
				bail()
				return
			}
			name, r := x.exprFor(call.Args[0])
			if r == unresolvable {
				// exprFor has already noted why.
				yield(&unresolvedTests{call.Pos(), 0})
				return
			}
			callback, r := x.exprFor(call.Args[1])
			if r == unresolvable {
				callback = nil
			}

			yield(&TestCall{name, callback, call.Pos()})

		case *ast.BlockStmt:
			// Recurse into (plain) blocks.
			for _, stmt := range stmt.List {
				if !yieldAll(x.findSubTests(tb, stmt), yield) {
					return
				}
			}

		case *ast.RangeStmt:
			if stmt.Body == nil {
				return
			}

			// We only support range statements where the key and value are
			// identifiers that the statement itself declares.
			key, kOK := stmt.Key.(*ast.Ident)
			val, vOK := stmt.Value.(*ast.Ident)
			if stmt.Key != nil && !kOK || stmt.Value != nil && !vOK ||
				(stmt.Key != nil || stmt.Value != nil) && stmt.Tok != token.DEFINE {
				bail()
				return
			}

			// We only support range operands whose underlying type is int,
			// slice, array, or map.
			typ := x.TypesInfo.TypeOf(stmt.X)
			if typ == nil {
				bail()
				return
			}
			switch typ := typ.Underlying().(type) {
			case *types.Basic:
				switch typ.Kind() {
				case types.UntypedInt,
					types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
					types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
				default:
					bail()
					return
				}
			case *types.Slice, *types.Array, *types.Map:
			default:
				bail()
				return
			}

			op, r := x.exprFor(stmt.X)
			if r == unresolvable {
				bail()
				return
			}

			children := slices.Collect(x.findSubTests(tb, stmt.Body))
			if len(children) == 0 {
				return
			}

			yield(&TestRange{
				key:      x.TypesInfo.Defs[key],
				val:      x.TypesInfo.Defs[val],
				x:        op,
				children: children,
			})

		default:
			bail()
		}
	}
}

// callsRun reports whether node contains a call to tb.Run.
//
// This search is intraprocedural, which leaves a known hole: a subtest created
// by a helper, as in
//
//	func TestFoo(t *testing.T) {
//		t.Run("a", func(t *testing.T) {})
//		helper(t) // calls t.Run("b", ...)
//		t.Run("c", func(t *testing.T) {})
//	}
//
// is neither enumerated nor detected. That is worse than merely missing a name:
// the undetected subtest still consumes a name, so if the helper's subtest
// collides with a sibling, every subsequent "#NN" suffix we report is shifted.
// We would report "a" and "c" when the true names may be "a", "b" and "c#01".
//
// Closing this requires following the [testing.T] value across call boundaries,
// which is out of scope for this analyzer (see the comment in findSubTests
// about recursing into arbitrary functions). Everything intraprocedural is
// handled: statements we cannot model but that provably contain a tb.Run call
// yield an [unresolvedTests] marker, which suppresses the whole sibling group.
func (x *Context) callsRun(tb types.Object, node ast.Node) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fun, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || fun.Sel.Name != "Run" {
			return true
		}
		recv, ok := fun.X.(*ast.Ident)
		if ok && x.TypesInfo.ObjectOf(recv) == tb {
			found = true
			return false
		}
		return true
	})
	return found
}

func (t *TestDecl) Eval(ctx *Context) iter.Seq[Test] {
	return func(yield func(Test) bool) {
		yield(Test{t.Name.Name, &FuncExpr{t.Type, t.Body}, t.Name.Pos(), true})
	}
}

func (t *unresolvedTests) Eval(ctx *Context) iter.Seq[Test] {
	return func(yield func(Test) bool) {
		ctx.pending |= t.why
		yield(Test{at: t.pos})
	}
}

func (t *TestCall) Eval(ctx *Context) iter.Seq[Test] {
	return func(yield func(Test) bool) {
		result := Test{at: t.pos}

		// Resolve the name. Failure means we know a subtest exists but not
		// what it is called, which invalidates its siblings' names too.
		expr, r := t.name.Eval(ctx)
		c, isConst := expr.(*Const)
		if r != resolved || !isConst || c.typ.Kind() != types.String {
			ctx.pending |= reasonDynamic
			yield(result)
			return
		}
		result.name = constant.StringVal(c.val)
		result.ok = true

		// Resolve the callback. Failure only means we can't recurse into it,
		// which is a plain false negative.
		if t.fn != nil {
			if fn, r := t.fn.Eval(ctx); r == resolved {
				result.fn, _ = fn.(*FuncExpr)
			}
		}

		yield(result)
	}
}

func (t *TestRange) Eval(ctx *Context) iter.Seq[Test] {
	return func(yield func(Test) bool) {
		x, r := t.x.Eval(ctx)
		if r != resolved {
			// t.x.Eval has already noted why.
			yield(Test{at: t.pos()})
			return
		}

		var seq iter.Seq2[Expression, Expression]
		switch x := x.(type) {
		case *Const:
			if x.val.Kind() != constant.Int {
				ctx.pending |= reasonUnsupported
				yield(Test{at: t.pos()})
				return
			}
			n, ok := constant.Uint64Val(x.val)
			if !ok {
				ctx.pending |= reasonUnsupported
				yield(Test{at: t.pos()})
				return
			}
			seq = func(yield func(Expression, Expression) bool) {
				for i := range n {
					if !yield(&Const{x.typ, constant.MakeUint64(i)}, nil) {
						return
					}
				}
			}

		case Slice:
			seq = func(yield func(Expression, Expression) bool) {
				intTyp := types.Typ[types.Int]
				for i, v := range x {
					if !yield(&Const{intTyp, constant.MakeInt64(int64(i))}, v) {
						return
					}
				}
			}

		case *Map:
			// Map iteration order is random at run time, but we are
			// enumerating a set of names, so any order will do as long as it
			// is deterministic. Sort by the key's exact constant form.
			order := make([]int, len(x.keys))
			for i := range order {
				order[i] = i
			}
			key := func(i int) string {
				if c, ok := x.keys[i].(*Const); ok {
					return c.val.ExactString()
				}
				return ""
			}
			slices.SortStableFunc(order, func(a, b int) int {
				return strings.Compare(key(a), key(b))
			})
			seq = func(yield func(Expression, Expression) bool) {
				for _, i := range order {
					if !yield(x.keys[i], x.vals[i]) {
						return
					}
				}
			}

		default:
			ctx.pending |= reasonUnsupported
			yield(Test{at: t.pos()})
			return
		}

		for k, v := range seq {
			// Bind the loop variables for this iteration only, then restore
			// the previous bindings so they cannot leak into later code that
			// refers to the same variable.
			undoKey := ctx.bind(t.key, k)
			undoVal := ctx.bind(t.val, v)

			ok := true
			for _, expr := range t.children {
				if !yieldAll(expr.Eval(ctx), yield) {
					ok = false
					break
				}
			}

			undoVal()
			undoKey()
			if !ok {
				return
			}
		}
	}
}

// pos returns a position to attribute an unresolved range to.
func (t *TestRange) pos() token.Pos {
	if t.key != nil {
		return t.key.Pos()
	}
	if t.val != nil {
		return t.val.Pos()
	}
	if len(t.children) > 0 {
		if c, ok := t.children[0].(*TestCall); ok {
			return c.pos
		}
	}
	return token.NoPos
}

// bind binds obj to expr, returning a function that restores the previous
// binding.
func (x *Context) bind(obj types.Object, expr Expression) func() {
	if obj == nil || expr == nil {
		return func() {}
	}
	prev, had := x.Values[obj]
	x.Values[obj] = expr
	return func() {
		if had {
			x.Values[obj] = prev
		} else {
			delete(x.Values, obj)
		}
	}
}

// debugf records that the analyzer could not interpret a construct, and why.
//
// The reason is accumulated for -stats. The message is reported as a
// diagnostic only when -explain is set; those are development aids, not
// diagnostics.
func (x *Context) debugf(why reason, pos token.Pos, format string, args ...any) {
	x.pending |= why
	if x.debug {
		x.Reportf(pos, format, args...)
	}
}
