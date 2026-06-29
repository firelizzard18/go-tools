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

var Analyzer = &analysis.Analyzer{
	Name:     "testfuncs",
	Doc:      analyzerutil.MustExtractDoc(doc, "testfuncs"),
	Requires: []*analysis.Analyzer{inspect.Analyzer, buildssa.Analyzer},
	Run:      run,
	URL:      "https://pkg.go.dev/golang.org/x/tools/gopls/internal/analysis/testfuncs",
}

type (
	Context struct {
		*analysis.Pass
		Inspect *inspector.Inspector
		SSA     *buildssa.SSA
		Names   map[string]int
		Values  map[*ast.Ident]Expression
	}

	Test struct {
		name string
		fn   *FuncExpr
		at   token.Pos
	}

	TestExpr interface {
		Eval(*Context) iter.Seq[Test]
	}

	TestDecl ast.FuncDecl

	TestCall struct {
		name, fn Expression
		pos      token.Pos
	}

	TestRange struct {
		key, val *ast.Ident
		x        Expression
		children []TestExpr
	}
)

func run(pass *analysis.Pass) (any, error) {
	x := &Context{
		Pass:    pass,
		Inspect: pass.ResultOf[inspect.Analyzer].(*inspector.Inspector),
		SSA:     pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA),
		Names:   map[string]int{},
		Values:  map[*ast.Ident]Expression{},
	}

	// Find top-level tests.
	for _, f := range x.Files {
		// Only analyze test files.
		if !strings.HasSuffix(x.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}

		x.Inspect.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
			decl := n.(*ast.FuncDecl)
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

			x.reportTest("", (*TestDecl)(decl))
		})
	}
	return nil, nil
}

func (x *Context) reportTest(prefix string, expr TestExpr) {
	for test := range expr.Eval(x) {
		fullName := x.uniqueName(prefix, test.name)
		x.Reportf(test.at, "Found: %s", fullName)

		if test.fn == nil {
			continue
		}

		for expr := range x.findSubTestsOf(test.fn.typ, test.fn.body) {
			x.reportTest(fullName+"/", expr)
		}
	}
}

func (x *Context) findSubTestsOf(typ *ast.FuncType, body *ast.BlockStmt) iter.Seq[TestExpr] {
	return func(yield func(TestExpr) bool) {
		// If the [testing.T] parameter is unnamed, the func cannot call
		// [testing.T.Run] and thus cannot create any subtests.
		if len(typ.Params.List) != 1 ||
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
		var call *ast.CallExpr
		var ok bool
		switch stmt := stmt.(type) {
		case *ast.ExprStmt:
			// An ExprStmt must be a call or a channel receive. So, CallExpr is the
			// only ExprStmt we care about.
			call, ok = stmt.X.(*ast.CallExpr)
			if !ok {
				return
			}

			// Recursing into arbitrary functions and methods is explicitly out of
			// scope, so all we care about here is `TB.Run` calls. Additionally, we only
			// care about calls where the receiver is the parent scope's `tb`.
			fun, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || fun.Sel.Name != "Run" {
				return
			}
			recv, ok := fun.X.(*ast.Ident)
			if !ok || x.TypesInfo.ObjectOf(recv) != tb {
				return
			}

			if len(call.Args) != 2 {
				return
			}
			name, ok := x.exprFor(call.Args[0])
			callback, _ := x.exprFor(call.Args[1])
			if !ok {
				return
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
			// identifiers.
			key, kOK := stmt.Key.(*ast.Ident)
			val, vOK := stmt.Value.(*ast.Ident)
			if stmt.Key != nil && !kOK || stmt.Value != nil && !vOK {
				return
			}

			// We only support range operands who's underlying type is int,
			// slice, or map.
			typ := x.TypesInfo.TypeOf(stmt.X)
			if typ == nil {
				return
			}
			typ = typ.Underlying()
			switch typ := typ.(type) {
			case *types.Basic:
				switch typ.Kind() {
				case types.UntypedInt,
					types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
					types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
				default:
					return
				}
			case *types.Slice, *types.Map:
			default:
				return
			}

			op, ok := x.exprFor(stmt.X)
			if !ok {
				return
			}

			children := slices.Collect(x.findSubTests(tb, stmt.Body))
			if len(children) == 0 {
				return
			}

			yield(&TestRange{
				key:      key,
				val:      val,
				x:        op,
				children: children,
			})

		default:
			// Unsupported statement type.
		}
	}
}

func (t *TestDecl) Eval(ctx *Context) iter.Seq[Test] {
	return func(yield func(Test) bool) {
		yield(Test{t.Name.Name, &FuncExpr{t.Type, t.Body}, t.Name.Pos()})
	}
}

func (t *TestCall) Eval(ctx *Context) iter.Seq[Test] {
	return func(yield func(Test) bool) {
		var result Test
		result.at = t.pos

		// Resolve the name. Failure = abort.
		if expr, ok := t.name.Eval(ctx); !ok {
			return // name is unresolvable (e.g. an unsupported expression)
		} else if c, ok := expr.(*Const); !ok {
			return // name is unresolvable (e.g. an unresolved phi)
		} else if c.typ.Kind() != types.String {
			return // name is not a string (how?)
		} else {
			result.name = constant.StringVal(c.val)
		}

		// Resolve the callback. Failure = can't recurse.
		if t.fn != nil {
			if x, ok := t.fn.Eval(ctx); !ok {
				// fn is unresolvable (e.g. an unsupported expression)
			} else if fn, ok := x.(*FuncExpr); !ok {
				// name is unresolvable (e.g. an unresolved phi or invalid type)
			} else {
				result.fn = fn
			}
		}

		yield(result)
	}
}

func (t *TestRange) Eval(ctx *Context) iter.Seq[Test] {
	return func(yield func(Test) bool) {
		x, ok := t.x.Eval(ctx)
		if !ok {
			return
		}

		var seq iter.Seq2[Expression, Expression]
		switch x := x.(type) {
		case *Const:
			if x.val.Kind() != constant.Int {
				panic(fmt.Errorf("cannot range over %v", x.val.Kind()))
			}
			seq = func(yield func(Expression, Expression) bool) {
				n, _ := constant.Uint64Val(x.val)
				for i := range n {
					if !yield(&Const{x.typ, constant.Make(i)}, Unknown{}) {
						return
					}
				}
			}

		case Slice:
			seq = func(yield func(Expression, Expression) bool) {
				intTyp := types.Typ[types.Int]
				for i, v := range x {
					if !yield(&Const{intTyp, constant.Make(i)}, v) {
						return
					}
				}
			}

		default:
			return // cannot resolve range operand
		}

		for k, v := range seq {
			if t.key != nil {
				ctx.Values[t.key] = k
			}
			if t.val != nil {
				ctx.Values[t.val] = v
			}
			for _, expr := range t.children {
				if !yieldAll(expr.Eval(ctx), yield) {
					return
				}
			}
		}
	}
}
