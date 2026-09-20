package testfuncs

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"iter"

	"golang.org/x/tools/go/ast/edge"
	"golang.org/x/tools/go/ast/inspector"
)

type (
	value interface {
		isValue()
	}

	seqValue interface {
		value
		All() iter.Seq2[value, value]
	}

	constValue   struct{ constant.Value }
	keyValuePair [2]value
	mapValue     []keyValuePair
	sliceValue   []value
	structValue  map[*types.Var]value
)

func (constValue) isValue()   {}
func (keyValuePair) isValue() {}
func (mapValue) isValue()     {}
func (sliceValue) isValue()   {}
func (structValue) isValue()  {}
func (*testFunc) isValue()    {}

func (v sliceValue) All() iter.Seq2[value, value] {
	return func(yield func(value, value) bool) {
		for i, v := range v {
			i := constValue{constant.MakeInt64(int64(i))}
			if !yield(i, v) {
				return
			}
		}
	}
}

func (v mapValue) All() iter.Seq2[value, value] {
	return func(yield func(value, value) bool) {
		for _, kv := range v {
			if !yield(kv[0], kv[1]) {
				return
			}
		}
	}
}

func evaluateAs[T value](x *Context, expr ast.Expr, cur inspector.Cursor, env map[*types.Var]value) (T, error) {
	// TODO(ethan.reesor): make this a generic method once gopls updates to Go 1.27.
	v, err := x.evaluate(expr, cur, env)
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

func (x *Context) evaluate(expr ast.Expr, cur inspector.Cursor, env map[*types.Var]value) (value, error) {
	tv := x.TypesInfo.Types[expr]
	if tv.IsType() {
		return nil, errorf(errUnmodeled, "type expressions are not supported")
	}
	if tv.Value != nil {
		if _, ok := tv.Type.(*types.Basic); ok {
			return constValue{tv.Value}, nil
		}
		return nil, errorf(errUnmodeled, "named-type constant values are not supported")
	}

	switch expr := expr.(type) {
	case *ast.ParenExpr:
		return x.evaluate(expr.X, cur.ChildAt(edge.ParenExpr_X, -1), env)

	case *ast.UnaryExpr:
		if expr.Op != token.AND {
			return nil, errorf(errUnmodeled, "unsupported unary operation: %v", expr.Op)
		}

		// Passthrough, we don't care about pointers.
		return x.evaluate(expr.X, cur.ChildAt(edge.UnaryExpr_X, -1), env)

	case *ast.StarExpr:
		// Passthrough, we don't care about pointers.
		return x.evaluate(expr.X, cur.ChildAt(edge.StarExpr_X, -1), env)

	case *ast.Ident:
		v := x.TypesInfo.Uses[expr]
		if v == nil {
			return nil, errorf(errUnresolved, "cannot resolve %v", expr.Name)
		}

		switch v := v.(type) {
		case *types.Var:
			return x.resolveVar(v, cur, env)

		case *types.Func:
			fn, ok := x.FuncDecls[v]
			if !ok {
				return nil, errorf(errUnresolved, "cannot resolve %v (func decl)", expr.Name)
			}
			return fn, nil
		}

		return nil, errorf(errUnresolved, "cannot resolve %v (%T)", expr.Name, v)

	case *ast.FuncLit:
		fn, ok := x.FuncLits[expr]
		if !ok {
			return nil, errorf(errUnresolved, "cannot resolve function literal")
		}
		return fn, nil

	case *ast.KeyValueExpr:
		k, e1 := x.evaluate(expr.Key, cur.ChildAt(edge.KeyValueExpr_Key, -1), env)
		v, e2 := x.evaluate(expr.Value, cur.ChildAt(edge.KeyValueExpr_Value, -1), env)
		return keyValuePair{k, v}, cmp.Or(e1, e2)

	case *ast.CompositeLit:
		// Structs need special handling.
		typ := derefType(tv.Type)
		if typ, ok := typ.(*types.Struct); ok {
			return x.evaluateStruct(typ, expr.Elts, cur, env)
		}

		values := make([]value, len(expr.Elts))
		for i, elt := range expr.Elts {
			v, err := x.evaluate(elt, cur.ChildAt(edge.CompositeLit_Elts, i), env)
			if err != nil {
				return nil, err
			}
			values[i] = v
		}

		switch typ.(type) {
		case *types.Slice, *types.Array:
			// Supporting indexed entries (KeyValueExpr) requires supporting
			// building the zero value of a given type (which we don't currently
			// support).
			for _, v := range values {
				if _, ok := v.(keyValuePair); ok {
					return nil, errorf(errUnmodeled, "indexed slice entries are not supported")
				}
			}
			return sliceValue(values), nil

		case *types.Map:
			v := make(mapValue, len(values))
			for i, u := range values {
				u, ok := u.(keyValuePair)
				if !ok {
					return nil, errorf(errInvalid, "missing key in map literal")
				}
				v[i] = u
			}
			return v, nil

		default:
			return nil, errorf(errUnmodeled, "unsupported composite literal type: %v", tv.Type)
		}

	case *ast.SelectorExpr:
		sel := x.TypesInfo.Selections[expr]
		if sel == nil || sel.Kind() != types.FieldVal {
			return nil, errorf(errUnresolved, "cannot resolve selector")
		}
		v, err := x.evaluate(expr.X, cur.ChildAt(edge.SelectorExpr_X, -1), env)
		if err != nil {
			return nil, err
		}
		typ := sel.Recv()
		for _, i := range sel.Index() {
			st, ok1 := derefType(typ).(*types.Struct)
			u, ok2 := v.(structValue)
			if !ok1 || !ok2 {
				return nil, errorf(errUnknown, "cannot resolve struct field")
			}

			f := st.Field(i)
			typ = f.Type()

			v, ok1 = u[f]
			if !ok1 {
				return nil, errorf(errUnmodeled, "field %s not set", f.Name())
			}
		}
		return v, nil
	}
	return nil, errorf(errUnmodeled, "unsupported expression: %T", expr)
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
			return nil, errorf(errInvalid, "invalid struct literal: cannot use %T as a field name", kv.Key)
		}
		named++
	}
	switch {
	case named > 0 && ordered > 0:
		return nil, errorf(errInvalid, "invalid struct literal: mixed named and unnamed fields")

	case ordered > 0:
		if ordered != typ.NumFields() {
			return nil, errorf(errInvalid, "invalid struct literal: wrong number of fields")
		}

		v := make(structValue)
		for i, elt := range elts {
			u, err := x.evaluate(elt, cur.ChildAt(edge.CompositeLit_Elts, i), vars)
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
				return nil, errorf(errInvalid, "invalid struct literal: %v is not a valid field name", kv.Key)
			case 1:
				// Ok
			default:
				// Supporting embedded field selectors (Go 1.27) would make this
				// significantly more complicated.
				return nil, errorf(errUnmodeled, "embedded field selectors are not supported")
			}

			u, err := x.evaluate(kv.Value, cur.ChildAt(edge.CompositeLit_Elts, i).ChildAt(edge.KeyValueExpr_Value, -1), vars)
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
		return nil, errorf(errUnmodeled, "not a local var: %v", v.Name())
	}

	// Find the enclosing function (there must be one).
	fn, _ := first(cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)))

	// Find all references to the variable.
	var refs []inspector.Cursor
	var defined bool
	for c := range fn.Preorder((*ast.Ident)(nil)) {
		if v != x.TypesInfo.ObjectOf(c.Node().(*ast.Ident)) {
			continue
		}

		switch parent := c.Parent().Node().(type) {
		case *ast.RangeStmt:
			if parent.Tok == token.DEFINE &&
				(c.ParentEdgeKind() == edge.RangeStmt_Key ||
					c.ParentEdgeKind() == edge.RangeStmt_Value) {
				defined = true
			}

		case *ast.ValueSpec:
			if c.ParentEdgeKind() == edge.ValueSpec_Names {
				defined = true
				if len(parent.Values) == 0 {
					continue
				}
			}

		case *ast.AssignStmt:
			if parent.Tok == token.DEFINE &&
				c.ParentEdgeKind() == edge.AssignStmt_Lhs {
				defined = true
			}
		}

		refs = append(refs, c)
	}

	// The only case we support:
	//
	//  - The variable is defined within the enclosing function.
	//  - There is exactly one write.
	//  - There is exactly one read.
	//  - The write precedes the read.
	//  - The write is not within anything (such as an if/for/etc).
	//
	if !defined || len(refs) != 2 {
		return nil, errorf(errUnmodeled, "cannot determine value of %v", v.Name())
	}

	switch stmt := refs[0].Parent().Node().(type) {
	case *ast.RangeStmt:
		// Key|Value < Range
		if !(refs[0].ParentEdgeKind() == edge.RangeStmt_Key ||
			refs[0].ParentEdgeKind() == edge.RangeStmt_Value) ||
			stmt.Tok != token.DEFINE {
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
		if refs[0].ParentEdgeKind() != edge.AssignStmt_Lhs ||
			refs[0].Parent().ParentEdgeKind() != edge.BlockStmt_List ||
			refs[0].Parent().Parent().Parent() != fn ||
			len(stmt.Lhs) != len(stmt.Rhs) ||
			stmt.Tok != token.ASSIGN && stmt.Tok != token.DEFINE {
			break
		}

		i := refs[0].ParentEdgeIndex()
		return x.evaluate(stmt.Rhs[i], refs[0].Parent().ChildAt(edge.AssignStmt_Rhs, i), env)

	case *ast.ValueSpec:
		// Names < ValueSpec < GenDecl < DeclStmt < Block < Func
		if refs[0].ParentEdgeKind() != edge.ValueSpec_Names ||
			refs[0].Parent().Parent().Parent().ParentEdgeKind() != edge.BlockStmt_List ||
			refs[0].Parent().Parent().Parent().Parent().Parent() != fn ||
			len(stmt.Names) != len(stmt.Values) {
			break
		}

		i := refs[0].ParentEdgeIndex()
		return x.evaluate(stmt.Values[i], refs[0].Parent().ChildAt(edge.ValueSpec_Values, i), env)
	}

	return nil, errorf(errUnresolved, "cannot determine value of %v", v.Name())
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
