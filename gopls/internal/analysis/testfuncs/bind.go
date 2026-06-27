package testfuncs

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/ssa"
)

func (x *Context) bindTest(test *Test) (*Test, bool) {
	expr, ok := test.Eval(x)
	return expr.(*Test), ok
}

func (x *Context) resolve(ident *ast.Ident) (Expression, bool) {
	if expr, ok := x.Values[ident]; ok {
		return expr, true
	}
	expr, ok := x.resolveOnce(ident)
	for ok && !expr.IsResolved() {
		expr, ok = expr.Eval(x)
	}
	if ok {
		x.Values[ident] = expr
	}
	return expr, ok
}

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

		if val, ok := val.(*ssa.Const); ok {
			if typ, ok := val.Type().(*types.Basic); ok {
				return &Const{typ: typ, val: val.Value}, true
			}

			// TODO: Report?
			return nil, false
		}

		// Walk that back to the AST declaration and generate an expression.
		node := x.pathEnclosingInterval(val.Pos(), val.Pos())[0]
		return x.exprFor(node)

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
