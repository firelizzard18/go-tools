package testfuncs

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/ssa"
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
	var expr Expression
	var ok bool
	obj := x.TypesInfo.ObjectOf(ident)
	switch obj := obj.(type) {
	case *types.Var:
		// Get the SSA value.
		path := x.pathEnclosingInterval(ident.Pos(), ident.Pos())
		val, _ := x.SSA.Pkg.Prog.VarValue(obj, x.SSA.Pkg, path)
		if val == nil {
			return false
		}

		if val, ok := val.(*ssa.Const); ok {
			if typ, ok := val.Type().(*types.Basic); ok {
				expr = &Const{typ: typ, val: val.Value}
				break
			}

			// TODO: Report?
			return false
		}

		// Walk that back to the AST declaration and generate an expression.
		node := x.pathEnclosingInterval(val.Pos(), val.Pos())[0]
		expr, ok = x.exprFor(node)
		if !ok {
			return false
		}

	case *types.Func:
		fn := x.SSA.Pkg.Prog.FuncValue(obj)
		expr, ok = x.exprFor(fn.Syntax())
		if !ok {
			return false
		}

	default:
		return false
	}

	x.Values[ident] = expr
	return true
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
