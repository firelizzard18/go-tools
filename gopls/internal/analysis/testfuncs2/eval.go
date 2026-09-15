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

	constValue   struct{ constant.Value }
	keyValuePair [2]value
	mapValue     []keyValuePair
	sliceValue   []value
	structValue  map[*types.Var]value

	funcValue struct {
		typ  *ast.FuncType
		body inspector.Cursor
	}
)

func (constValue) isValue()   {}
func (funcValue) isValue()    {}
func (keyValuePair) isValue() {}
func (mapValue) isValue()     {}
func (sliceValue) isValue()   {}
func (structValue) isValue()  {}

func evaluateAs[T value](x *Context, expr ast.Expr, vars map[*types.Var]value, cur inspector.Cursor) (T, error) {
	// TODO(ethan.reesor): make this a generic method once gopls updates to Go 1.27.
	v, err := x.evaluate(expr, vars, cur)
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

func (x *Context) evaluate(expr ast.Expr, vars map[*types.Var]value, cur inspector.Cursor) (value, error) {
	tv := x.TypesInfo.Types[expr]
	if tv.IsType() {
		return nil, fmt.Errorf("type expressions are not supported")
	}
	if tv.Value != nil {
		if _, ok := tv.Type.(*types.Basic); ok {
			return constValue{tv.Value}, nil
		}
		return nil, fmt.Errorf("named-type constant values are not supported")
	}

	switch expr := expr.(type) {
	case *ast.ParenExpr:
		return x.evaluate(expr.X, vars, cur.ChildAt(edge.ParenExpr_X, -1))
	case *ast.UnaryExpr:
		if expr.Op != token.AND {
			return nil, fmt.Errorf("unsupported unary operation: %v", expr.Op)
		}

		// Passthrough, we don't care about pointers.
		return x.evaluate(expr.X, vars, cur.ChildAt(edge.UnaryExpr_X, -1))

	case *ast.StarExpr:
		// Passthrough, we don't care about pointers.
		return x.evaluate(expr.X, vars, cur.ChildAt(edge.StarExpr_X, -1))

	case *ast.Ident:
		// TODO: Check for package-level functions.

		v, ok := x.TypesInfo.Uses[expr].(*types.Var)
		if !ok {
			return nil, fmt.Errorf("%v is not a variable", expr.Name)
		}
		if u, ok := vars[v]; ok {
			return u, nil
		}
		return nil, fmt.Errorf("cannot determine value of %v", expr.Name)

	case *ast.FuncLit:
		return funcValue{expr.Type, cur.ChildAt(edge.FuncLit_Body, -1)}, nil

	case *ast.KeyValueExpr:
		k, e1 := x.evaluate(expr.Key, vars, cur.ChildAt(edge.KeyValueExpr_Key, -1))
		v, e2 := x.evaluate(expr.Value, vars, cur.ChildAt(edge.KeyValueExpr_Value, -1))
		return keyValuePair{k, v}, cmp.Or(e1, e2)

	case *ast.CompositeLit:
		// Structs need special handling.
		typ := derefType(tv.Type)
		if typ, ok := typ.(*types.Struct); ok {
			return x.evaluateStruct(typ, expr.Elts, vars, cur)
		}

		values := make([]value, len(expr.Elts))
		for i, elt := range expr.Elts {
			v, err := x.evaluate(elt, vars, cur.ChildAt(edge.CompositeLit_Elts, i))
			if err != nil {
				return nil, err
			}
			values[i] = v
		}

		switch typ.(type) {
		case *types.Slice:
			panic("TODO")

		case *types.Array:
			panic("TODO")

		case *types.Map:
			panic("TODO")

		default:
			return nil, fmt.Errorf("unsupported composite literal type: %v", tv.Type)
		}

	case *ast.SelectorExpr:
		sel := x.TypesInfo.Selections[expr]
		if sel == nil || sel.Kind() != types.FieldVal {
			return nil, fmt.Errorf("cannot resolve selector")
		}
		v, err := x.evaluate(expr.X, vars, cur.ChildAt(edge.SelectorExpr_X, -1))
		if err != nil {
			return nil, err
		}
		typ := sel.Recv()
		for _, i := range sel.Index() {
			st, ok1 := derefType(typ).(*types.Struct)
			u, ok2 := v.(structValue)
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("cannot resolve struct field")
			}

			f := st.Field(i)
			typ = f.Type()

			v, ok1 = u[f]
			if !ok1 {
				return nil, fmt.Errorf("field %s not set", f.Name())
			}
		}
		return v, nil
	}
	return nil, fmt.Errorf("unsupported expression: %T", expr)
}

func (x *Context) evaluateStruct(typ *types.Struct, elts []ast.Expr, vars map[*types.Var]value, cur inspector.Cursor) (structValue, error) {
	var named, ordered int
	for _, elt := range elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			ordered++
			continue
		}

		if _, ok := kv.Key.(*ast.Ident); !ok {
			return nil, fmt.Errorf("invalid struct literal: cannot use %T as a field name", kv.Key)
		}
		named++
	}
	switch {
	case named > 0 && ordered > 0:
		return nil, fmt.Errorf("invalid struct literal: mixed named and unnamed fields")

	case ordered > 0:
		if ordered != typ.NumFields() {
			return nil, fmt.Errorf("invalid struct literal: wrong number of fields")
		}

		v := make(structValue)
		for i, elt := range elts {
			u, err := x.evaluate(elt, vars, cur.ChildAt(edge.CompositeLit_Elts, i))
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
			u, err := x.evaluate(kv.Value, vars, cur.ChildAt(edge.CompositeLit_Elts, i).ChildAt(edge.KeyValueExpr_Value, -1))
			if err != nil {
				return nil, err
			}

			f, ok := x.TypesInfo.Uses[kv.Key.(*ast.Ident)].(*types.Var)
			if !ok {
				return nil, fmt.Errorf("invalid struct literal: %v is not a valid field name", kv.Key)
			}
			v[f] = u
		}
		return v, nil
	}
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
