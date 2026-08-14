// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testfuncs

import (
	"go/ast"
	"go/types"
)

// resolveVarAST resolves a variable in AST mode, using syntax and type
// information only.
func (x *Context) resolveVarAST(ident *ast.Ident, obj *types.Var) (Expression, resolution) {
	init, ok := x.initExprFor(obj)
	if !ok {
		x.debugf(ident.Pos(), "%v is not assigned exactly once", ident.Name)
		return nil, unresolvable
	}

	// Syntax cannot distinguish a read of a variable from a write through it,
	// nor a value that is merely passed to a function from one that is
	// mutated by it. In place of that analysis we require that the variable is
	// mentioned exactly twice in the entire package: once where it is declared
	// and once where it is used (the reference we are resolving).
	//
	// Counting across the whole package, rather than only the enclosing
	// function, is what makes a package-level variable mutated from TestMain
	// or from a helper fail to resolve.
	//
	// The heuristic is deliberately over-conservative: a completely harmless
	// third mention, such as len(tcs), disqualifies the variable. That is the
	// direction in which it is safe to be wrong.
	if n := x.refs[obj]; n != 2 {
		x.debugf(ident.Pos(), "%v has %d references; only an unaliased, unmodified variable can be resolved", ident.Name, n)
		return nil, unresolvable
	}

	return x.exprFor(init.expr)
}
