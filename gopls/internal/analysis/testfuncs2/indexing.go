package testfuncs

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/ast/edge"
	"golang.org/x/tools/go/ast/inspector"
)

type (
	indicies struct {
		FuncDecls map[*types.Func]*testFunc
		FuncLits  map[*ast.FuncLit]*testFunc
	}

	testFunc struct {
		Body   inspector.Cursor
		Type   *types.Signature
		Params map[*types.Var]*testParam
	}

	testParam struct {
		Refs    map[inspector.Cursor]tbRefKind
		Escapes bool // A runnable param escapes into a closure, etc.
	}

	tbRefKind int
)

const (
	safeTBRef tbRefKind = iota
	unsafeTBRef
	tbRunCall
	tbAsCallArg
)

func (x *Context) buildIndices() {
	// Capture the test function call graph.
	x.FuncDecls = make(map[*types.Func]*testFunc)
	x.FuncLits = make(map[*ast.FuncLit]*testFunc)
outer:
	for decl := range x.Inspect.Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		fn := &testFunc{
			Params: make(map[*types.Var]*testParam),
		}

		switch node := decl.Node().(type) {
		case *ast.FuncDecl:
			if node.Body == nil {
				continue
			}
			fn.Type = x.TypesInfo.Defs[node.Name].(*types.Func).Signature()
			fn.Body = decl.ChildAt(edge.FuncDecl_Body, -1)

		case *ast.FuncLit:
			if node.Body == nil {
				continue
			}
			fn.Type = x.TypesInfo.Types[node].Type.(*types.Signature)
			fn.Body = decl.ChildAt(edge.FuncLit_Body, -1)
		}

		// Gather any runnable parameters. If there are none, skip this
		// function.
		//
		// If there's a variadic runnable parameter, skip this function. This
		// will cause callers to pessimistically assume the function might
		// create subtests; that's what we want since we're not tracing variadic
		// access.
		for i := range fn.Type.Params().Len() {
			if !isRunnableParam(fn.Type, i) {
				continue
			}
			if fn.Type.Variadic() && i == fn.Type.Params().Len()-1 {
				continue outer
			}
			fn.Params[fn.Type.Params().At(i)] = &testParam{
				Refs: make(map[inspector.Cursor]tbRefKind),
			}
		}
		if len(fn.Params) == 0 {
			continue
		}

		// Ensure the vars are not captured within closures or assigned to
		// runnable fields or variables.
		for cur := range fn.Body.Preorder((*ast.Ident)(nil)) {
			v, ok := x.TypesInfo.Uses[cur.Node().(*ast.Ident)].(*types.Var)
			if !ok {
				continue
			}

			p, ok := fn.Params[v]
			if !ok {
				continue
			}

			enc, _ := first(cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)))
			if enc != decl {
				p.Escapes = true
				continue
			}

			// If V is assigned to a runnable field or variable (or we can't
			// determine the type), consider it an escape.
			switch cur.ParentEdgeKind() {
			case edge.AssignStmt_Rhs:
				i := cur.ParentEdgeIndex()
				stmt := cur.Parent().Node().(*ast.AssignStmt)
				if i < len(stmt.Lhs) {
					p.Escapes = true
					break
				}

				typ := x.TypesInfo.TypeOf(stmt.Lhs[i])
				if typ == nil || isRunnable(typ) {
					p.Escapes = true
				}

			case edge.ValueSpec_Values:
				i := cur.ParentEdgeIndex()
				stmt := cur.Parent().Node().(*ast.ValueSpec)
				if i < len(stmt.Names) {
					p.Escapes = true
					break
				}

				typ := x.TypesInfo.TypeOf(stmt.Names[i])
				if typ == nil || isRunnable(typ) {
					p.Escapes = true
				}
			}
		}

		// Find the calls.
		fn.Body.Inspect([]ast.Node{(*ast.Ident)(nil), (*ast.FuncLit)(nil)}, func(cur inspector.Cursor) (descend bool) {
			// Do not descend into function literals.
			if _, ok := cur.Node().(*ast.FuncLit); ok {
				return false
			}

			// Is this one of our vars?
			v, ok := x.TypesInfo.Uses[cur.Node().(*ast.Ident)].(*types.Var)
			if !ok {
				return true
			}

			p, ok := fn.Params[v]
			if !ok {
				return true
			}

			switch cur.ParentEdgeKind() {
			case edge.CallExpr_Args:
				// Passed as an argument to a call.
				//
				// If the parameter V is being passed to is runnable (or if we
				// can't determine the function signature), record the call
				call := cur.Parent().Node().(*ast.CallExpr)
				typ, ok := x.TypesInfo.TypeOf(call.Fun).(*types.Signature)
				if !ok || isRunnableParam(typ, cur.ParentEdgeIndex()) {
					p.Refs[cur] = tbAsCallArg
				}

			case edge.SelectorExpr_X:
				// Accessing a method.
				//
				// If the method isn't Run or RunParallel, we don't care.
				// Descend to ensure we catch cleanup callbacks,
				// `t.Log(t.Run(...))`, etc.
				switch cur.Parent().Node().(*ast.SelectorExpr).Sel.Name {
				default:
					return true
				case "Run", "RunParallel":
				}

				// If the parent is not a CallExpr, something weird is
				// happening.
				if cur.Parent().ParentEdgeKind() != edge.CallExpr_Fun {
					p.Refs[cur] = unsafeTBRef
					break
				}

				p.Refs[cur] = tbRunCall

			case edge.AssignStmt_Rhs:
				// Already folded into Escapes, so ignore.

			default:
				// Consider anything else to be unsafe.
				p.Refs[cur] = unsafeTBRef
			}
			return true
		})

		switch node := decl.Node().(type) {
		case *ast.FuncDecl:
			x.FuncDecls[x.TypesInfo.Defs[node.Name].(*types.Func)] = fn
		case *ast.FuncLit:
			x.FuncLits[node] = fn
		}
	}
}

func isRunnableParam(fn *types.Signature, i int) bool {
	var isVar bool
	if fn.Variadic() && i >= fn.Params().Len()-1 {
		isVar, i = true, fn.Params().Len()-1
	}
	if i >= fn.Params().Len() {
		// Assume invalid parameters are runnable.
		return true
	}

	v := fn.Params().At(i)
	if isVar {
		return isRunnable(v.Type().(*types.Slice).Elem())
	}
	return isRunnable(v.Type())
}

// isRunnable checks if T or *T has a `Run(string, func(TB))` method.
func isRunnable(typ types.Type) bool {
	if _, ok := tbKind(typ); ok {
		return true
	}

	// If T is not a pointer or interface, use *T to ensure we capture pointer
	// methods.
	switch typ.Underlying().(type) {
	case *types.Pointer, *types.Interface:
	default:
		typ = types.NewPointer(typ)
	}

	for m := range types.NewMethodSet(typ).Methods() {
		switch m.Obj().Name() {
		case "Run":
		case "RunParallel":
			// TODO: Actually check if it has the right signature.
			return true
		default:
			continue
		}

		typ := m.Type().(*types.Signature)
		if typ.Variadic() || typ.Params().Len() != 2 {
			continue
		}
		if !types.Identical(typ.Params().At(0).Type(), types.Typ[types.String]) {
			continue
		}

		sig, ok := typ.Params().At(1).Type().(*types.Signature)
		if !ok {
			continue
		}
		_, ok = testKind(sig)
		if ok {
			return true
		}
	}
	return false
}
