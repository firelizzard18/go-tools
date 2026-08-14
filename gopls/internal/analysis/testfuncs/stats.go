// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testfuncs

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// reportStats emits the summary record for the test function the analyzer has
// just finished, and clears it.
//
// The message is a sequence of space-separated key=value pairs following
// "<FuncName>: ". Keys appear in a fixed order and values never contain
// spaces, so the record can be parsed by splitting. The position of the
// diagnostic identifies the package, file, line, and column; none of that is
// repeated in the message.
func (x *Context) reportStats() {
	r := x.record
	x.record, x.pending = nil, 0

	var b strings.Builder
	fmt.Fprintf(&b, "%s: runs=%d subtests=%d status=%s", r.name, r.runs, r.subtests, r.status())
	if why := r.reasons.String(); r.status() != statusComplete && why != "" {
		fmt.Fprintf(&b, " reason=%s", why)
	}
	x.Reportf(r.pos, "%s", b.String())
}

// status classifies how completely the function's subtests were enumerated.
//
// It is derived from the [unresolvedTests] markers produced by findSubTests
// and from the resolution failures reported by [TestCall.Eval] and
// [TestRange.Eval], all of which funnel through [Context.unresolved].
func (r *statsRecord) status() status {
	switch {
	case !r.unresolved:
		return statusComplete // includes the runs=0 case
	case r.subtests > 0:
		return statusPartial
	default:
		return statusNone
	}
}

// unresolved records that some construct below the top-level function could
// not be enumerated, and commits the reasons accumulated since the last test
// was successfully resolved.
func (x *Context) unresolved() {
	if x.record == nil {
		return
	}
	x.record.unresolved = true
	x.record.reasons |= x.pending
	x.pending = 0
}

// countRuns counts the calls to testing.[TBF].Run that appear lexically within
// decl, including those in nested function literals.
//
// This is a syntactic count, not the number of subtests enumerated: it is the
// denominator against which coverage is measured. The receiver must be an
// identifier denoting an object of type *testing.[TBF]; a call through any
// other expression (a struct field, say) is not counted, since the analyzer
// could not have followed it either.
func (x *Context) countRuns(decl *ast.FuncDecl) int {
	n := 0
	ast.Inspect(decl, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		fun, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || fun.Sel.Name != "Run" {
			return true
		}
		recv, ok := fun.X.(*ast.Ident)
		if !ok {
			return true
		}
		obj := x.TypesInfo.ObjectOf(recv)
		if obj == nil {
			return true
		}
		ptr, ok := obj.Type().(*types.Pointer)
		if !ok {
			return true
		}
		named, ok := ptr.Elem().(*types.Named)
		if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "testing" {
			return true
		}
		switch named.Obj().Name() {
		case "T", "B", "F":
			n++
		}
		return true
	})
	return n
}

// bailReason classifies a statement that provably contains a call to tb.Run
// but that the analyzer does not model.
//
// If the call is nested inside a function literal, the statement hands control
// (and the [testing.TB]) to someone else, which is the interprocedural hole
// described on [Context.callsRun]. Otherwise the statement form itself is the
// obstacle.
func (x *Context) bailReason(tb types.Object, stmt ast.Stmt) reason {
	inLit := false
	var visit func(ast.Node, bool) bool
	visit = func(node ast.Node, lit bool) bool {
		found := false
		ast.Inspect(node, func(n ast.Node) bool {
			if found {
				return false
			}
			if fn, ok := n.(*ast.FuncLit); ok {
				if visit(fn.Body, true) {
					found = true
				}
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
				inLit = lit
				return false
			}
			return true
		})
		return found
	}

	visit(stmt, false)
	if inLit {
		return reasonInterprocedural
	}
	return reasonUnsupported
}

// String returns the most specific reason in the set, or "" if it is empty.
func (r reason) String() string {
	for _, c := range []struct {
		bit  reason
		name string
	}{
		{reasonInterprocedural, "interprocedural"},
		{reasonMutable, "mutable"},
		{reasonNoBinding, "no-binding"},
		{reasonDynamic, "dynamic"},
		{reasonUnsupported, "unsupported"},
	} {
		if r&c.bit != 0 {
			return c.name
		}
	}
	return ""
}

// statsRecord accumulates the summary of a single top-level test function.
type statsRecord struct {
	name string
	pos  token.Pos

	// runs is the syntactic count of tb.Run calls; see [Context.countRuns].
	runs int

	// subtests is the number of subtest names enumerated below the top-level
	// function, at any depth.
	subtests int

	// unresolved records whether any construct could not be enumerated, and
	// reasons is the set of categories committed by [Context.unresolved].
	unresolved bool
	reasons    reason
}

// status is the coverage classification of a test function.
type status string

const (
	// statusComplete means every subtest was enumerated. A function with no
	// tb.Run calls at all is complete.
	statusComplete status = "complete"

	// statusPartial means at least one subtest was enumerated and at least one
	// construct was not.
	statusPartial status = "partial"

	// statusNone means nothing below the top-level function was enumerated.
	statusNone status = "none"
)

// reason is a set of coarse categories explaining why enumeration failed. It
// is a set, not a single value, because one failed group may have several
// causes; [reason.String] reduces it to the most specific one.
type reason uint

const (
	// reasonMutable: the value could not be proven immutable and unaliased.
	reasonMutable reason = 1 << iota

	// reasonNoBinding: there is no single definite binding site.
	reasonNoBinding

	// reasonDynamic: the value depends on a call or expression the analyzer
	// cannot evaluate.
	reasonDynamic

	// reasonUnsupported: a statement or expression form that is not modeled.
	reasonUnsupported

	// reasonInterprocedural: subtests are created through another function.
	reasonInterprocedural
)
