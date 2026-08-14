// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testfuncs

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// resolveVarSSA resolves a variable in hybrid mode.
//
// The value itself is read from the syntax of the variable's initializer, as
// in AST mode. SSA is used for the part syntax cannot do: proving that nothing
// writes to the variable, or to the memory it designates, after it has been
// initialized. Without that proof the syntax is a lie, because
//
//	tcs := []struct{ name string }{{name: "before"}}
//	tcs[0].name = "after"
//
// still looks like "before" to a purely syntactic reader.
func (x *Context) resolveVarSSA(ident *ast.Ident, obj *types.Var) (Expression, resolution) {
	path := x.pathEnclosingInterval(ident.Pos(), ident.Pos())
	if path == nil {
		return nil, unresolvable
	}

	val, isAddr := x.SSA.Pkg.Prog.VarValue(obj, x.SSA.Pkg, path)
	if val == nil {
		x.debugf(ident.Pos(), "Unable to resolve value of %v", ident.Name)
		return nil, unresolvable
	}

	// When isAddr is clear, val is the value itself rather than the address of
	// the variable's storage, so a constant is already the answer.
	if c, ok := val.(*ssa.Const); ok && !isAddr {
		return x.resolveConst(ident, c.Type(), c.Value)
	}

	init, ok := x.initExprFor(obj)
	if !ok {
		x.debugf(ident.Pos(), "%v is not assigned exactly once", ident.Name)
		return nil, unresolvable
	}

	// A value of basic type held in a register needs no audit: a string,
	// number, or bool has no interior that anyone could write to and no
	// identity that anyone could alias.
	_, isBasic := val.Type().Underlying().(*types.Basic)
	if isAddr || !isBasic {
		// A composite literal is built by allocating storage, storing the
		// elements into it, and (for a slice) slicing the result. Auditing the
		// allocation covers the slice too, since the slice instruction is one
		// of the allocation's referrers.
		root := val
		if slice, ok := val.(*ssa.Slice); ok {
			if alloc, ok := slice.X.(*ssa.Alloc); ok {
				root = alloc
			}
		}

		if !x.immutable(root, init.stmt, map[ssa.Value]bool{}) {
			x.debugf(ident.Pos(), "%v may be modified after it is initialized", ident.Name)
			return nil, unresolvable
		}
	}

	return x.exprFor(init.expr)
}

// immutable reports whether the memory designated by v is written to only
// within init, and whether v escapes to anywhere that could write to it.
//
// It is deliberately a whitelist: any instruction it does not recognize as a
// read is treated as a potential write. Failing to resolve a variable costs a
// test name; wrongly believing a variable is constant produces a name that
// does not exist.
func (x *Context) immutable(v ssa.Value, init ast.Node, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return true
	}
	seen[v] = true

	refs := v.Referrers()
	if refs == nil {
		return true
	}

	for _, instr := range *refs {
		switch instr := instr.(type) {
		case *ssa.DebugRef:
			// Metadata; not a real use.

		case *ssa.UnOp:
			// Only a load. The loaded copy may be modified freely; that does
			// not affect the original.
			if instr.Op != token.MUL {
				return false
			}

		case *ssa.FieldAddr:
			if !x.immutable(instr, init, seen) {
				return false
			}

		case *ssa.IndexAddr:
			if !x.immutable(instr, init, seen) {
				return false
			}

		case *ssa.Slice:
			if !x.immutable(instr, init, seen) {
				return false
			}

		case *ssa.Store:
			// Storing v elsewhere creates an alias we cannot follow.
			if instr.Val == v {
				return false
			}
			// Storing through v is a write; it is only benign if it is part
			// of the variable's initialization.
			if !within(init, instr.Pos()) {
				return false
			}

		case *ssa.MapUpdate:
			if instr.Map != v || !within(init, instr.Pos()) {
				return false
			}

		case *ssa.Range, *ssa.Next:
			// Iteration reads.

		case *ssa.BinOp:
			// Comparison.

		case *ssa.Call:
			// A call may modify or retain its argument. len and cap are the
			// exceptions, and len is how a range over a slice is compiled.
			b, ok := instr.Call.Value.(*ssa.Builtin)
			if !ok || (b.Name() != "len" && b.Name() != "cap") {
				return false
			}

		default:
			return false
		}
	}
	return true
}
