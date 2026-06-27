package testfuncs

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/ssa"
)

func (x *Context) resolve(ident *ast.Ident) (Expression, bool) {
	if expr, ok := x.Values[ident]; ok {
		return expr, true
	}

	// Resolve the identifier. This returns nil on failure so we need to exit.
	expr, ok := x.resolveOnce(ident)
	if !ok {
		return nil, false
	}

	// Keep on attempting to resolve until we have a fully resolved expression,
	// or resolution fails (e.g. due to an unsupported expression).
	for ok && !expr.IsResolved() {
		expr, ok = expr.Eval(x)
	}

	// Cache the result.
	x.Values[ident] = expr
	return expr, ok
}

// resolveOnce uses SSA to locate the value of the identifier at the given
// location in the code. Resolution fails if SSA returns a phi or other
// indeterminate result.
func (x *Context) resolveOnce(ident *ast.Ident) (Expression, bool) {
	obj := x.TypesInfo.ObjectOf(ident)
	switch obj := obj.(type) {
	case *types.Var:
		// Get the SSA value.
		path := x.pathEnclosingInterval(ident.Pos(), ident.Pos())
		val, _ := x.SSA.Pkg.Prog.VarValue(obj, x.SSA.Pkg, path)
		if val == nil {
			return nil, false
		}

		switch val := val.(type) {
		case *ssa.Phi:
			// The value of a phi is indeterminate (we don't support branching
			// outside of specific scenarios).
			return nil, false

		case *ssa.Const:
			if typ, ok := val.Type().(*types.Basic); ok {
				return &Const{typ: typ, val: val.Value}, true
			}

			// TODO: Report?
			return nil, false

		default:
			// Walk that back to the AST declaration and generate an expression.
			node := x.pathEnclosingInterval(val.Pos(), val.Pos())[0]
			return x.exprFor(node)
		}

	case *types.Func:
		fn := x.SSA.Pkg.Prog.FuncValue(obj)
		return x.exprFor(fn.Syntax())

	default:
		return nil, false
	}
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
