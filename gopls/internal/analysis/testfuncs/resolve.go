// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testfuncs

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ast/astutil"
)

// binding records a site at which a variable is assigned a value.
type binding struct {
	// expr is the assigned expression, or nil if the assignment does not have
	// the form "v = <expr>" (for example "a, b := f()").
	expr ast.Expr

	// stmt is the enclosing AssignStmt or ValueSpec. Stores generated for the
	// initialization of the variable fall within this range; stores outside it
	// are mutations.
	stmt ast.Node
}

// resolve returns the value of the object that ident refers to.
//
// The two analyzer modes share everything here except the resolution of
// variables, which is dispatched to resolve_ssa.go or resolve_ast.go.
func (x *Context) resolve(ident *ast.Ident) (Expression, resolution) {
	obj := x.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return nil, unresolvable
	}

	// A range variable has no single value: it is bound by [TestRange.Eval],
	// one iteration at a time. If it is not bound right now, defer, so that
	// the enclosing expression is re-evaluated once it is.
	if x.rangeVars[obj] {
		if expr, ok := x.Values[obj]; ok {
			return expr.Eval(x)
		}
		return &Ident{ident}, deferred
	}

	switch obj := obj.(type) {
	case *types.Const:
		return x.resolveConst(ident, obj.Type(), obj.Val())

	case *types.Func:
		return x.resolveFunc(obj)

	case *types.Var:
		// A package-level variable may be written by any function in the
		// package, by an init function, or (if exported) by another package
		// entirely. Its declaration says nothing about its value at the time
		// the test runs.
		if obj.Pkg() != nil && obj.Parent() == obj.Pkg().Scope() {
			x.debugf(reasonNoBinding, ident.Pos(), "%v is a package-level variable", ident.Name)
			return nil, unresolvable
		}

		if x.mode == modeAST {
			return x.resolveVarAST(ident, obj)
		}
		return x.resolveVarSSA(ident, obj)

	default:
		x.debugf(reasonUnsupported, ident.Pos(), "Unable to resolve %v: unsupported object %T", ident.Name, obj)
		return nil, unresolvable
	}
}

func (x *Context) resolveConst(ident *ast.Ident, typ types.Type, val constant.Value) (Expression, resolution) {
	if typ, ok := typ.Underlying().(*types.Basic); ok {
		return &Const{typ: typ, val: val}, resolved
	}

	x.debugf(reasonUnsupported, ident.Pos(), "%v resolves to an unsupported type: %v", ident.Name, typ)
	return nil, unresolvable
}

// resolveFunc resolves a function or method to its syntax. This is done
// through positions rather than SSA so that both modes behave identically, and
// so that a function with no body (or one declared in another package) simply
// fails to resolve instead of panicking.
func (x *Context) resolveFunc(obj *types.Func) (Expression, resolution) {
	for _, n := range x.pathEnclosingInterval(obj.Pos(), obj.Pos()) {
		switch n := n.(type) {
		case *ast.FuncDecl:
			if n.Body == nil {
				return nil, unresolvable
			}
			return &FuncExpr{n.Type, n.Body}, resolved
		case *ast.FuncLit:
			return &FuncExpr{n.Type, n.Body}, resolved
		}
	}
	x.debugf(reasonUnsupported, obj.Pos(), "Unable to locate the body of %v", obj.Name())
	return nil, unresolvable
}

// initExprFor returns the sole binding of obj.
//
// If obj is assigned in more than one place, resolution fails. This is what
// makes conditional initialization ("var v; if c { v = a } else { v = b }")
// and accumulation ("v = append(v, ...)") safe: picking any one of the
// bindings would produce names the test never runs.
func (x *Context) initExprFor(obj types.Object) (binding, bool) {
	b, ok := x.bindings[obj]
	if !ok || len(b) != 1 || b[0].expr == nil {
		return binding{}, false
	}
	return b[0], true
}

// pathEnclosingInterval returns the AST path enclosing [start, end), or nil if
// the interval does not lie within a file of this package.
func (x *Context) pathEnclosingInterval(start, end token.Pos) []ast.Node {
	if !start.IsValid() {
		return nil
	}
	tf := x.Fset.File(start)
	if tf == nil {
		return nil
	}
	for _, f := range x.Files {
		if x.Fset.File(f.Pos()) != tf {
			continue
		}
		path, _ := astutil.PathEnclosingInterval(f, start, end)
		return path
	}
	return nil
}

// within reports whether pos lies inside node's source range.
func within(node ast.Node, pos token.Pos) bool {
	return pos.IsValid() && node.Pos() <= pos && pos < node.End()
}
