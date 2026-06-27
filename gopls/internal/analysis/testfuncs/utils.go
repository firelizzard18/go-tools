package testfuncs

import "iter"

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
