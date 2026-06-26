package testfuncs

import (
	_ "embed"
	"go/ast"
	"go/types"
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

func run(pass *analysis.Pass) (any, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	for _, f := range pass.Files {
		// Only analyze test files.
		if !strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}

		inspect.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
			decl := n.(*ast.FuncDecl)

			obj, ok := pass.TypesInfo.ObjectOf(decl.Name).(*types.Func)
			if !ok || !obj.Exported() {
				return
			}

			// error.Error has empty Position, PkgPath, and ObjectPath.
			if obj.Pkg() == nil {
				return
			}

			isTest, isExample := isTestOrExample(obj)
			if !isTest && !isExample {
				return
			}

			pass.Reportf(decl.Pos(), "Found: %s", obj.Name())
		})
	}

	return nil, nil
}

// isTestOrExample reports whether the given func is a testing func or an
// example func (or neither). isTestOrExample returns (true, false) for testing
// funcs, (false, true) for example funcs, and (false, false) otherwise.
func isTestOrExample(fn *types.Func) (isTest, isExample bool) {
	sig := fn.Type().(*types.Signature)
	if sig.Params().Len() == 0 &&
		sig.Results().Len() == 0 {
		return false, isTestName(fn.Name(), "Example")
	}

	kind, ok := testKind(sig)
	if !ok {
		return false, false
	}
	switch kind.Name() {
	case "T":
		return isTestName(fn.Name(), "Test"), false
	case "B":
		return isTestName(fn.Name(), "Benchmark"), false
	case "F":
		return isTestName(fn.Name(), "Fuzz"), false
	default:
		return false, false // "can't happen" (see testKind)
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
