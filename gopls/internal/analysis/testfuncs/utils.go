package testfuncs

import (
	"go/types"
	"iter"
	"strings"
	"unicode"
	"unicode/utf8"
)

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

func allResolved[E Expression](exprs []E) bool {
	for _, expr := range exprs {
		if !expr.IsResolved() {
			return false
		}
	}
	return true
}

func yieldAll[V any](it iter.Seq[V], yield func(V) bool) bool {
	for v := range it {
		if !yield(v) {
			return false
		}
	}
	return true
}

func evalAll(ctx *Context, in []Expression) ([]Expression, bool) {
	out := make([]Expression, len(in))
	allOk := true
	for i, v := range in {
		var ok bool
		out[i], ok = v.Eval(ctx)
		if !ok {
			allOk = false
		}
	}
	return out, allOk
}
