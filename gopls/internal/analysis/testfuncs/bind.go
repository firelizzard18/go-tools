package testfuncs

import (
	"go/ast"
	"go/types"
)

func (x *Context) bind(expr Expression) Expression {
	for ref := range expr.Needs() {
		if _, ok := x.Values[ref]; ok {
			continue
		}

		if !x.resolve(ref) {
			return expr
		}
	}

	return expr.Bind(x)
}

func (x *Context) resolve(ident *ast.Ident) bool {
	obj := x.TypesInfo.ObjectOf(ident)
	switch obj := obj.(type) {
	case *types.Func:
		fn := x.SSA.Pkg.Prog.FuncValue(obj)
		expr, ok := x.exprFor(fn.Syntax())
		if !ok {
			return false
		}
		x.Values[ident] = expr
		return true
	}

	return false
}
