package testfuncs

import (
	"go/ast"
	"go/constant"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

func findTBParam(pass *analysis.Pass, typ *ast.FuncType) (types.Object, bool) {
	// If the [testing.T] parameter is unnamed, the func cannot call
	// [testing.T.Run] and thus cannot create any subtests
	if len(typ.Params.List[0].Names) == 0 {
		return nil, false
	}

	// This "can't fail" because testKind should guarantee that the function has
	// one parameter and the check above guarantees that parameter is named
	param := pass.TypesInfo.ObjectOf(typ.Params.List[0].Names[0])
	return param, true
}

func findSubtests(pass *analysis.Pass, tb types.Object, parent string, stmt ast.Stmt) {
	var call *ast.CallExpr
	var ok bool
	switch stmt := stmt.(type) {
	case *ast.ExprStmt:
		// An ExprStmt must be a call or a channel receive. So, CallExpr is the
		// only ExprStmt we care about.
		call, ok = stmt.X.(*ast.CallExpr)
		if !ok {
			return
		}

	default:
		// Unsupported statement type.
		return
	}

	// Is it a `tb.Run` call (where tb matches the receiver provided to us)?
	// Recursing into arbitrary functions and methods is explicitly out of
	// scope.
	if len(call.Args) != 2 {
		return
	}
	fun, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || fun.Sel.Name != "Run" {
		return
	}
	recv, ok := fun.X.(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(recv) != tb {
		return
	}

	sig, ok := pass.TypesInfo.TypeOf(call.Args[1]).(*types.Signature)
	if !ok {
		return
	}
	if _, ok := testKind(sig); !ok {
		return // subtest has wrong signature
	}

	val := pass.TypesInfo.Types[call.Args[0]].Value // may be zero
	if val == nil || val.Kind() != constant.String {
		return
	}

	pass.Reportf(stmt.Pos(), "Subtest: %s/%s", parent, constant.StringVal(val))
}
