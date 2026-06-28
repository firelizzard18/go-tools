package testfuncs

import (
	_ "embed"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"iter"
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
)

func run(pass *analysis.Pass) (any, error) {
	x := &Context{
		Pass:    pass,
		Inspect: pass.ResultOf[inspect.Analyzer].(*inspector.Inspector),
		SSA:     pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA),
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

			x.report("", (*TestDecl)(decl))
		})
	}
	return nil, nil
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

		case *ast.BlockStmt:
			for _, stmt := range stmt.List {
				if !yieldAll(x.findSubTests(tb, stmt), yield) {
					return
				}
			}

		default:
			// Unsupported statement type.
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
		name, ok1 := x.exprFor(call.Args[0])
		callback, ok2 := x.exprFor(call.Args[1])
		if !ok1 || !ok2 {
			return
		}

		if !yield(&TestCall{name, callback, call.Pos()}) {
			return
		}
	}
}

func (x *Context) report(prefix string, expr TestExpr) {
	for test := range expr.Eval(x) {
		fullName := prefix + test.name
		x.Reportf(test.at, "Found: %s", fullName)

		if test.fn == nil {
			continue
		}

		// If the [testing.T] parameter is unnamed, the func cannot call
		// [testing.T.Run] and thus cannot create any subtests.
		if len(test.fn.typ.Params.List) != 1 ||
			len(test.fn.typ.Params.List[0].Names) == 0 {
			return
		}

		// This "can't fail" because testKind should guarantee that the function has
		// one parameter and the check above guarantees that parameter is named
		tb := x.TypesInfo.ObjectOf(test.fn.typ.Params.List[0].Names[0])

		for _, stmt := range test.fn.body.List {
			for expr := range x.findSubTests(tb, stmt) {
				x.report(fullName+"/", expr)
			}
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
