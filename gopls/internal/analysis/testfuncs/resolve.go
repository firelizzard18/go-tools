package testfuncs

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/ssa"
)

// resolveOnce uses SSA to locate the value of the identifier at the given
// location in the code. Resolution fails if SSA returns a phi or other
// indeterminate result.
func (x *Context) resolve(ident *ast.Ident) (Expression, bool) {
	if expr, ok := x.Values[ident]; ok {
		return expr, true
	}

	obj := x.TypesInfo.ObjectOf(ident)
	switch obj := obj.(type) {
	case *types.Const:
		return x.resolveConst(ident, obj.Type(), obj.Val())

	case *types.Func:
		fn := x.SSA.Pkg.Prog.FuncValue(obj)
		return x.exprFor(fn.Syntax())

	case *types.Var:
		// Get the SSA value.
		path := x.pathEnclosingInterval(ident.Pos(), ident.Pos())
		val, _ := x.SSA.Pkg.Prog.VarValue(obj, x.SSA.Pkg, path)
		if val == nil {
			x.Reportf(ident.Pos(), "Unable to resolve value of %v", ident.Name)
			return nil, false
		}

		return x.resolveSSA(ident, val)

	default:
		x.Reportf(ident.Pos(), "Unable to resolve %v: unsupported object %T", ident.Name, obj)
		return nil, false
	}
}

func (x *Context) resolveSSA(ident *ast.Ident, val ssa.Value) (Expression, bool) {
	// If the SSA value corresponds to an AST node, find it.
	var path []ast.Node
	if val.Pos() != token.NoPos {
		path = x.pathEnclosingInterval(val.Pos(), val.Pos())
	}

	switch val := val.(type) {
	case *ssa.Phi:
		// The value of a phi is indeterminate. We'll return the identifier
		// because we might be able to resolve this later (for example,
		// within a table-driven test for-range statement).
		return &Ident{ident}, true

	case *ssa.Const:
		return x.resolveConst(ident, val.Type(), val.Value)

	case *ssa.UnOp:
		// Resolve through synthesized operations.
		if val.Pos() == token.NoPos {
			return x.resolveSSA(ident, val.X)
		}
		return x.exprFor(path[0])

	case *ssa.Slice, *ssa.Alloc:
		// Resolve to the AST expression.
		if path == nil {
			return nil, false
		}
		return x.exprFor(path[0])

	case *ssa.IndexAddr:
		// This is intentionally identical to the default case (minus the
		// reporting), in case we decide to add support for index expressions in
		// the future.
		//
		// The SSA for a ast.RangeStmt on a slice includes an ssa.IndexAddr
		// where Pos is ast.RangeStmt.X. `x.exprFor(path[0])` resolves to the
		// slice, which is definitely not what we want. We could resolve this to
		// a Selector (like we do for ast.IndexExpr) but that would make
		// TestRange unnecessarily complicated.
		//
		// TL;DR: To future readers, if you want to add support for
		// ssa.IndexAddr, you must carve out a special case for ast.RangeStmt.
		return &Ident{ident}, true

	default:
		x.Reportf(ident.Pos(), "%v resolves to unsupported SSA value (%T)%[2]v", ident.Name, val)
		return &Ident{ident}, true
	}
}

func (x *Context) resolveConst(ident *ast.Ident, typ types.Type, val constant.Value) (Expression, bool) {
	if typ, ok := typ.(*types.Basic); ok {
		return &Const{typ: typ, val: val}, true
	}

	x.Reportf(ident.Pos(), "%v resolves to an unsupported type: %v", ident.Name, typ)
	return nil, false
}

func (x *Context) pathEnclosingInterval(start, end token.Pos) []ast.Node {
	var file *ast.File
	want := x.Fset.File(start).Pos(0)
	for _, f := range x.Files {
		if f.Pos() == want {
			file = f
			goto ok
		}
	}
	panic("node does not belong to file set!")

ok:
	path, _ := astutil.PathEnclosingInterval(file, start, end)
	return path
}
