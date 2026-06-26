package testfuncs

import (
	_ "embed"
	"go/ast"
	"go/constant"
	"go/types"
	"iter"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

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
		Values  map[ast.Node]Expression
	}
)

func run(pass *analysis.Pass) (any, error) {
	x := &Context{
		Pass:    pass,
		Inspect: pass.ResultOf[inspect.Analyzer].(*inspector.Inspector),
		Values:  map[ast.Node]Expression{},
	}

	tests := slices.Collect(x.topLevel())
	for len(tests) > 0 {
		tests = slices.AppendSeq(tests[1:], x.report(tests[0]))
	}
	return nil, nil
}

func (x *Context) topLevel() iter.Seq[*Test] {
	return func(yield func(*Test) bool) {
		for _, f := range x.Files {
			// Only analyze test files.
			if !strings.HasSuffix(x.Fset.Position(f.Pos()).Filename, "_test.go") {
				continue
			}

			var aborted bool
			x.Inspect.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
				if aborted {
					return
				}

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

				name := &Const{types.Typ[types.String], constant.MakeString(obj.Name())}
				if !yield(&Test{"", name, &FuncDecl{obj.Signature(), decl}, decl.Pos()}) {
					aborted = true
				}
			})
		}
	}
}

func (x *Context) report(test *Test) iter.Seq[*Test] {
	return func(yield func(*Test) bool) {
		test := x.bind(test).(*Test)
		name, ok := test.name.(*Const)
		if !ok || name.val.Kind() != constant.String {
			return // Cannot resolve name
		}

		fullName := test.prefix + constant.StringVal(name.val)
		x.Reportf(test.pos, "Found: %s", fullName)

		fn, ok := test.fn.(FuncExpr)
		if !ok {
			return // Cannot resolve the callback
		}

		// If the [testing.T] parameter is unnamed, the func cannot call
		// [testing.T.Run] and thus cannot create any subtests. And an empty
		// body can't contain subtests.
		typ, body := fn.Func()
		if len(typ.Params.List) != 1 ||
			len(typ.Params.List[0].Names) == 0 ||
			body == nil {
			return
		}

		// This "can't fail" because testKind should guarantee that the function has
		// one parameter and the check above guarantees that parameter is named
		tb := x.TypesInfo.ObjectOf(typ.Params.List[0].Names[0])

		for _, stmt := range body.List {
			if !yieldAll(x.find(tb, fullName+"/", stmt), yield) {
				return
			}
		}
	}
}

func (x *Context) find(tb types.Object, prefix string, stmt ast.Stmt) iter.Seq[*Test] {
	return func(yield func(*Test) bool) {
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
		name, ok1 := x.exprForExpr(call.Args[0])
		callback, ok2 := x.exprForExpr(call.Args[1])
		if !ok1 || !ok2 {
			return
		}

		if !yield(&Test{prefix, name, callback, call.Pos()}) {
			return
		}
	}
}

// isTestOrExample reports whether the given func is a testing func or an
// example func (or neither). isTestOrExample returns (true, false) for testing
// funcs, (false, true) for example funcs, and (false, false) otherwise.
func isTestOrExample(fn *types.Func) bool {
	sig := fn.Type().(*types.Signature)
	if sig.Params().Len() == 0 &&
		sig.Results().Len() == 0 {
		return isTestName(fn.Name(), "Example")
	}

	kind, ok := testKind(sig)
	if !ok {
		return false
	}
	switch kind.Name() {
	case "T":
		return isTestName(fn.Name(), "Test")
	case "B":
		return isTestName(fn.Name(), "Benchmark")
	case "F":
		return isTestName(fn.Name(), "Fuzz")
	default:
		return false // "can't happen" (see testKind)
	}
}

// isTestName reports whether name is a valid test name for the test kind
// indicated by the given prefix ("Test", "Benchmark", etc.).
//
// Adapted from go/analysis/passes/tests.
func isTestName(name, prefix string) bool {
	suffix, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return false
	}
	if len(suffix) == 0 {
		// "Test" is ok.
		return true
	}
	r, _ := utf8.DecodeRuneInString(suffix)
	return !unicode.IsLower(r)
}

// testKind returns the parameter type TypeName of a test, benchmark, or fuzz
// function (one of testing.[TBF]).
func testKind(sig *types.Signature) (*types.TypeName, bool) {
	if sig.Params().Len() != 1 ||
		sig.Results().Len() != 0 {
		return nil, false
	}

	ptr, ok := sig.Params().At(0).Type().(*types.Pointer)
	if !ok {
		return nil, false
	}

	named, ok := ptr.Elem().(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "testing" {
		return nil, false
	}

	switch named.Obj().Name() {
	case "T", "B", "F":
		return named.Obj(), true
	}
	return nil, false
}
