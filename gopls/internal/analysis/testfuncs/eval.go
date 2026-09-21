package testfuncs

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ast/edge"
	"golang.org/x/tools/go/ast/inspector"
)

type (
	value interface {
		isValue()
	}

	constValue  struct{ constant.Value }
	seqValue    []seqEntry
	structValue map[*types.Var]value

	seqEntry struct {
		key, value value
		src        ast.Node
	}

	identRefKind int
)

const (
	identRefUnmodeled identRefKind = iota
	identRefRead
	identRefAssign
	identRefDefine
	identRefDeclare
)

func (constValue) isValue()  {}
func (seqValue) isValue()    {}
func (seqEntry) isValue()    {}
func (structValue) isValue() {}
func (*testFunc) isValue()   {}

func evaluateAs[T value](x *Context, cur inspector.Cursor, env map[*types.Var]value) (T, error) {
	// TODO(ethan.reesor): make this a generic method once gopls updates to Go 1.27.
	v, err := x.evaluate(cur, env)
	if err != nil {
		var z T
		return z, err
	}

	u, ok := v.(T)
	if !ok {
		var z T
		return z, fmt.Errorf("want %T, got %T", z, v)
	}
	return u, nil
}

func (x *Context) evaluate(cur inspector.Cursor, env map[*types.Var]value) (value, error) {
	expr := cur.Node().(ast.Expr)
	tv := x.TypesInfo.Types[expr]
	if tv.IsType() {
		return nil, errorf(cur.Node(), errUnmodeled, "type expressions are not supported")
	}
	if tv.Value != nil {
		if _, ok := tv.Type.(*types.Basic); ok {
			return constValue{tv.Value}, nil
		}
		return nil, errorf(cur.Node(), errUnmodeled, "named-type constant values are not supported")
	}

	switch expr := expr.(type) {
	case *ast.ParenExpr:
		return x.evaluate(cur.ChildAt(edge.ParenExpr_X, -1), env)

	case *ast.UnaryExpr:
		if expr.Op != token.AND {
			return nil, errorf(cur.Node(), errUnmodeled, "unsupported unary operation: %v", expr.Op)
		}

		// Passthrough, we don't care about pointers.
		return x.evaluate(cur.ChildAt(edge.UnaryExpr_X, -1), env)

	case *ast.StarExpr:
		// Passthrough, we don't care about pointers.
		return x.evaluate(cur.ChildAt(edge.StarExpr_X, -1), env)

	case *ast.Ident:
		v := x.TypesInfo.Uses[expr]
		if v == nil {
			return nil, errorf(cur.Node(), errUnresolved, "cannot resolve %v", expr.Name)
		}

		switch v := v.(type) {
		case *types.Var:
			return x.resolveVar(v, cur, env)

		case *types.Func:
			fn, ok := x.FuncDecls[v]
			if !ok {
				return nil, errorf(cur.Node(), errUnresolved, "cannot resolve %v (func decl)", expr.Name)
			}
			return fn, nil
		}

		return nil, errorf(cur.Node(), errUnresolved, "cannot resolve %v (%T)", expr.Name, v)

	case *ast.FuncLit:
		fn, ok := x.FuncLits[expr]
		if !ok {
			return nil, errorf(cur.Node(), errUnresolved, "cannot resolve function literal")
		}
		return fn, nil

	case *ast.KeyValueExpr:
		k, e1 := x.evaluate(cur.ChildAt(edge.KeyValueExpr_Key, -1), env)
		v, e2 := x.evaluate(cur.ChildAt(edge.KeyValueExpr_Value, -1), env)
		return seqEntry{k, v, cur.Node()}, cmp.Or(e1, e2)

	case *ast.CompositeLit:
		// Structs need special handling.
		typ := derefType(tv.Type)
		if typ, ok := typ.(*types.Struct); ok {
			return x.evaluateStruct(typ, expr.Elts, cur, env)
		}

		values := make([]value, len(expr.Elts))
		for i := range expr.Elts {
			v, err := x.evaluate(cur.ChildAt(edge.CompositeLit_Elts, i), env)
			if err != nil {
				return nil, err
			}
			values[i] = v
		}

		_, isMap := typ.(*types.Map)
		v := make(seqValue, 0, len(values))
		for i, u := range values {
			kv, isKV := u.(seqEntry)
			switch {
			case isMap:
				if !isKV {
					return nil, errorf(expr.Elts[i], errInvalid, "missing key in map literal")
				}
			case isKV:
				return nil, errorf(expr.Elts[i], errUnmodeled, "indexed slice entries are not supported")
			default:
				kv = seqEntry{
					key:   constValue{constant.MakeInt64(int64(i))},
					value: u,
					src:   expr.Elts[i],
				}
			}
			v = append(v, kv)
		}
		return v, nil

	case *ast.SelectorExpr:
		sel := x.TypesInfo.Selections[expr]
		if sel == nil || sel.Kind() != types.FieldVal {
			return nil, errorf(cur.Node(), errUnresolved, "cannot resolve selector")
		}
		v, err := x.evaluate(cur.ChildAt(edge.SelectorExpr_X, -1), env)
		if err != nil {
			return nil, err
		}
		typ := sel.Recv()
		for _, i := range sel.Index() {
			st, ok1 := derefType(typ).(*types.Struct)
			u, ok2 := v.(structValue)
			if !ok1 || !ok2 {
				return nil, errorf(cur.Node(), errUnknown, "cannot resolve struct field")
			}

			f := st.Field(i)
			typ = f.Type()

			v, ok1 = u[f]
			if !ok1 {
				return nil, errorf(cur.Node(), errUnmodeled, "field %s not set", f.Name())
			}
		}
		return v, nil
	}
	return nil, errorf(cur.Node(), errUnmodeled, "unsupported expression: %T", expr)
}

func (x *Context) evaluateStruct(typ *types.Struct, elts []ast.Expr, cur inspector.Cursor, vars map[*types.Var]value) (structValue, error) {
	var named, ordered int
	for _, elt := range elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			ordered++
			continue
		}

		if _, ok := kv.Key.(*ast.Ident); !ok {
			return nil, errorf(elt, errInvalid, "invalid struct literal: cannot use %T as a field name", kv.Key)
		}
		named++
	}
	switch {
	case named > 0 && ordered > 0:
		return nil, errorf(cur.Node(), errInvalid, "invalid struct literal: mixed named and unnamed fields")

	case ordered > 0:
		if ordered != typ.NumFields() {
			return nil, errorf(cur.Node(), errInvalid, "invalid struct literal: wrong number of fields")
		}

		v := make(structValue)
		for i := range elts {
			u, err := x.evaluate(cur.ChildAt(edge.CompositeLit_Elts, i), vars)
			if err != nil {
				return nil, err
			}
			v[typ.Field(i)] = u
		}
		return v, nil

	default:
		v := make(structValue)
		for i, elt := range elts {
			kv := elt.(*ast.KeyValueExpr)
			_, path, _ := types.LookupFieldOrMethod(typ, false, x.Pkg, kv.Key.(*ast.Ident).Name)
			switch len(path) {
			case 0:
				return nil, errorf(elt, errInvalid, "invalid struct literal: %v is not a valid field name", kv.Key)
			case 1:
				// Ok
			default:
				// Supporting embedded field selectors (Go 1.27) would make this
				// significantly more complicated.
				return nil, errorf(elt, errUnmodeled, "embedded field selectors are not supported")
			}

			u, err := x.evaluate(cur.ChildAt(edge.CompositeLit_Elts, i).ChildAt(edge.KeyValueExpr_Value, -1), vars)
			if err != nil {
				return nil, err
			}
			v[typ.Field(path[0])] = u
		}
		return v, nil
	}
}

func (x *Context) resolveVar(v *types.Var, cur inspector.Cursor, env map[*types.Var]value) (value, error) {
	// We don't support package variables.
	if v.Kind() != types.LocalVar {
		return nil, errorf(cur.Node(), errUnmodeled, "not a local var: %v", v.Name())
	}

	// Find the enclosing function (there must be one).
	fn, _ := first(cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)))

	// Given that table driven tests frequently use the table entry as both a
	// name and parameters for the test case, there are only two ways to handle
	// that:
	//  - Look for the single-write, single-read pattern, but ignore reads within the closure; or
	//  - Characterize whether a given statement is a read or a write.
	//
	// I argue that the latter is simpler.
	//
	// TODO: Determine if the scope is correct by using the Var to find the
	// scope (instead of enclosing) then comparing to the cursor's scope.

	var write inspector.Cursor
	var declared bool
	for c := range fn.Preorder((*ast.Ident)(nil)) {
		if v != x.TypesInfo.ObjectOf(c.Node().(*ast.Ident)) {
			continue
		}

		kind := characterizeIdentExpr(c, false)
		switch kind {
		case identRefRead:
			continue
		case identRefUnmodeled:
			// We report this as unresolved to signal "we were unable to resolve
			// this variable". "Due to an unmodeled expression/statement" is
			// secondary.
			return nil, errorf(c.Node(), errUnresolved, "cannot determine value of %v", v.Name())
		}

		// The write must come before the statement we're analyzing.
		if c.Node().Pos() > cur.Node().Pos() {
			return nil, errorf(c.Node(), errUnresolved, "cannot determine value of %v", v.Name())
		}

		// There must only be a single write, and a write before the definition
		// does not make sense.
		if write.Valid() {
			return nil, errorf(c.Node(), errUnresolved, "cannot determine value of %v", v.Name())
		}

		// We could detect redeclarations, but those will cause errors anyways.
		switch kind {
		case identRefDeclare:
			declared = true
		case identRefAssign:
			write = c
		case identRefDefine:
			declared = true
			write = c
		}
	}

	// The variable must be declared within the enclosing function and written
	// prior to the expression being resolved.
	if !declared || !write.Valid() {
		return nil, errorf(cur.Node(), errUnresolved, "cannot determine value of %v", v.Name())
	}

	switch stmt := write.Parent().Node().(type) {
	case *ast.RangeStmt:
		// Key|Value < Range
		if stmt.Tok != token.DEFINE {
			break
		}

		// The LHS of a RangeStmt is only resolvable via env.
		v, ok := env[v]
		if !ok {
			break
		}
		return v, nil

	case *ast.AssignStmt:
		// Lhs < Assign < Block < Func
		if write.Parent().ParentEdgeKind() != edge.BlockStmt_List ||
			write.Parent().Parent().Parent() != fn ||
			len(stmt.Lhs) != len(stmt.Rhs) ||
			stmt.Tok != token.ASSIGN && stmt.Tok != token.DEFINE {
			break
		}

		i := write.ParentEdgeIndex()
		return x.evaluate(write.Parent().ChildAt(edge.AssignStmt_Rhs, i), env)

	case *ast.ValueSpec:
		// Names < ValueSpec < GenDecl < DeclStmt < Block < Func
		if write.Parent().Parent().Parent().ParentEdgeKind() != edge.BlockStmt_List ||
			write.Parent().Parent().Parent().Parent().Parent() != fn ||
			len(stmt.Names) != len(stmt.Values) {
			break
		}

		i := write.ParentEdgeIndex()
		return x.evaluate(write.Parent().ChildAt(edge.ValueSpec_Values, i), env)
	}

	return nil, errorf(cur.Node(), errUnresolved, "cannot determine value of %v", v.Name())
}

func characterizeIdentExpr(cur inspector.Cursor, nested bool) identRefKind {
	node := cur.Parent().Node()
	switch cur.ParentEdgeKind() {
	case edge.ExprStmt_X, edge.RangeStmt_X,
		edge.CallExpr_Args,
		edge.BinaryExpr_X, edge.BinaryExpr_Y:
		return identRefRead

	case edge.SelectorExpr_X:
		return characterizeIdentExpr(cur.Parent(), true)

	case edge.RangeStmt_Key, edge.RangeStmt_Value:
		// If we're not at the root, an ident on the LHS of the range statement
		// is an unmodeled write.
		if nested {
			return identRefUnmodeled
		}
		if node.(*ast.RangeStmt).Tok == token.DEFINE {
			return identRefDefine
		}
		return identRefAssign

	case edge.ValueSpec_Names:
		if len(node.(*ast.ValueSpec).Values) == 0 {
			return identRefDeclare
		}
		return identRefDefine

	case edge.AssignStmt_Lhs:
		if node.(*ast.AssignStmt).Tok == token.DEFINE {
			return identRefDefine
		}
		// If we're not at the root, an ident on the LHS of a (non-define)
		// assign is an unmodeled write.
		if nested {
			return identRefUnmodeled
		}
		return identRefAssign
	}

	return identRefUnmodeled
}

func derefType(typ types.Type) types.Type {
	switch typ := typ.(type) {
	case *types.Named, *types.Alias:
		return derefType(typ.Underlying())
	case *types.Pointer:
		return derefType(typ.Elem())
	}
	return typ
}
