package testfuncs

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

type (
	Expression interface {
		Eval(*Context) (Expression, bool)
	}

	Unknown struct {
		// TODO: Replace me with nil
	}

	Const struct {
		typ *types.Basic
		val constant.Value
	}

	Ident struct {
		*ast.Ident
	}

	Selector struct {
		x   Expression
		sel Expression
	}

	Struct struct {
		typ    *types.Struct
		fields []Expression
	}

	Slice []Expression

	FuncExpr struct {
		typ  *ast.FuncType
		body *ast.BlockStmt
	}

	Sprintf struct {
		format Expression
		args   []Expression
	}
)

func (x *Context) exprFor(node ast.Node) (Expression, bool) {
	var expr ast.Expr
	switch node := node.(type) {
	case *ast.ExprStmt:
		expr = node.X
	case *ast.FuncDecl:
		return &FuncExpr{node.Type, node.Body}, true
	case ast.Expr:
		expr = node
	default:
		x.Reportf(node.Pos(), "Unable to resolve %T", node)
		return nil, false
	}

	// Is the value a constant?
	if tv, ok := x.TypesInfo.Types[expr]; ok && tv.Value != nil {
		if typ, ok := tv.Type.(*types.Basic); ok {
			return &Const{typ, tv.Value}, true
		}
	}

	var val Expression
	switch expr := expr.(type) {
	case *ast.FuncLit:
		val = &FuncExpr{expr.Type, expr.Body}

	case *ast.Ident:
		val = &Ident{expr}

	case *ast.SelectorExpr:
		y, ok := x.exprFor(expr.X)
		if !ok {
			return nil, false
		}
		val = &Selector{y, &Ident{expr.Sel}}

	case *ast.IndexExpr:
		y, ok1 := x.exprFor(expr.X)
		i, ok2 := x.exprFor(expr.Index)
		if !ok1 || !ok2 {
			return nil, false
		}
		val = &Selector{y, i}

	case *ast.CompositeLit:
		typ := x.TypesInfo.TypeOf(expr)
		return x.compositeExprFor(expr.Pos(), typ, expr.Elts)

	default:
		x.Reportf(expr.Pos(), "Unable to resolve %T", expr)
		return nil, false
	}

	// Eagerly resolve identifiers and selectors.
	return val.Eval(x)
}

func (x *Context) compositeExprFor(pos token.Pos, typ types.Type, elts []ast.Expr) (Expression, bool) {
	switch typ := typ.(type) {
	case *types.Named:
		// We don't care about the name (though if we want to support methods,
		// we will need to deal with it).
		return x.compositeExprFor(pos, typ.Underlying(), elts)

	case *types.Struct:
		s := &Struct{
			typ:    typ,
			fields: make([]Expression, typ.NumFields()),
		}

		for i, elt := range elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				key, ok := kv.Key.(*ast.Ident)
				if !ok {
					x.Reportf(pos, "Unable to resolve struct literal: %T is not a legal field name", kv.Key)
					return nil, false
				}

				i = findStructField(typ, key.Name)
				if i < 0 {
					x.Reportf(pos, "Unable to resolve struct literal: %q is not a field of %v", key.Name, typ)
					return nil, false
				}

				elt = kv.Value
			}

			v, ok := x.exprFor(elt)
			if ok {
				s.fields[i] = v
			}
		}

		for i := range s.fields {
			if s.fields[i] != nil {
				continue
			}

			v, ok := x.zeroFor(pos, typ.Field(i).Type())
			if ok {
				s.fields[i] = v
			} else {
				s.fields[i] = Unknown{}
			}
		}
		return s, true

	case *types.Slice:
		s := make(Slice, len(elts))
		for i, elt := range elts {
			v, ok := x.exprFor(elt)
			if ok {
				s[i] = v
			} else {
				s[i] = Unknown{}
			}
		}
		return s, true

	default:
		x.Reportf(pos, "Unable to resolve composite literal: %v not supported", typ)
		return nil, false
	}
}

func findStructField(typ *types.Struct, name string) int {
	for i := range typ.NumFields() {
		if typ.Field(i).Name() == name {
			return i
		}
	}
	return -1
}

func (x *Context) zeroFor(pos token.Pos, typ types.Type) (Expression, bool) {
	switch typ := typ.(type) {
	case *types.Named:
		// We don't care about the name (though if we want to support methods,
		// we will need to deal with it).
		return x.zeroFor(pos, typ.Underlying())

	default:
		x.Reportf(pos, "Cannot construct a zero value for unsupported type %v", typ)
		return nil, false
	}
}

func (Unknown) Eval(*Context) (Expression, bool)     { return Unknown{}, true }
func (v *Const) Eval(*Context) (Expression, bool)    { return v, true }
func (v *FuncExpr) Eval(*Context) (Expression, bool) { return v, true }

func (v *Ident) Eval(ctx *Context) (Expression, bool) {
	// This could resolve right back to an Ident if the value is potentially
	// resolvable (e.g. a phi from a for-range loop).
	if u, ok := ctx.resolve(v.Ident); ok {
		return u, true
	}
	return v, false
}

func (v *Selector) Eval(ctx *Context) (Expression, bool) {
	x, ok := v.x.Eval(ctx)
	v = &Selector{x: x, sel: v.sel}
	if !ok {
		return v, false
	}

	switch x := x.(type) {
	case *Struct:
		sel, ok := v.sel.(*Ident)
		if !ok {
			return nil, false // sel is not a struct field
		}
		if i := findStructField(x.typ, sel.Name); i >= 0 {
			return x.fields[i], true
		}

	case Slice:
		idx, ok := v.sel.(*Const)
		if !ok || idx.val.Kind() != constant.Int {
			return nil, false // sel is not an index
		}
		i, ok := constant.Uint64Val(idx.val)
		if !ok || i > uint64(len(x)) {
			return nil, false // out of bounds
		}
		return x[i], true
	}
	return v, true
}

func (v *Sprintf) Eval(ctx *Context) (Expression, bool) {
	format, ok1 := v.format.Eval(ctx)
	args, ok2 := evalAll(ctx, v.args)
	u := &Sprintf{format, args}
	if !ok1 || !ok2 {
		return u, false
	}

	fmtVal, ok := format.(*Const)
	if !ok {
		return u, true
	}

	argVals := make([]any, len(u.args))
	for i, arg := range u.args {
		arg, ok := arg.(*Const)
		if !ok {
			return u, true
		}
		argVals[i] = arg.Value()
	}

	s := fmt.Sprintf(fmtVal.Value().(string), argVals...)
	return &Const{
		typ: types.Typ[types.String],
		val: constant.MakeString(s),
	}, true
}

func (v *Struct) Eval(ctx *Context) (Expression, bool) {
	fields, ok := evalAll(ctx, v.fields)
	return &Struct{typ: v.typ, fields: fields}, ok
}

func (v Slice) Eval(ctx *Context) (Expression, bool) {
	u, ok := evalAll(ctx, v)
	return Slice(u), ok
}

func (v *Const) Value() any {
	switch v.typ.Kind() {
	case types.Bool:
		return constant.BoolVal(v.val)
	case types.String:
		return constant.StringVal(v.val)
	case types.Int:
		i, _ := constant.Int64Val(v.val)
		return int(i)
	case types.Int8:
		i, _ := constant.Int64Val(v.val)
		return int8(i)
	case types.Int16:
		i, _ := constant.Int64Val(v.val)
		return int16(i)
	case types.Int32:
		i, _ := constant.Int64Val(v.val)
		return int32(i)
	case types.Int64:
		i, _ := constant.Int64Val(v.val)
		return i
	case types.Uint:
		u, _ := constant.Uint64Val(v.val)
		return uint(u)
	case types.Uint8:
		u, _ := constant.Uint64Val(v.val)
		return uint8(u)
	case types.Uint16:
		u, _ := constant.Uint64Val(v.val)
		return uint16(u)
	case types.Uint32:
		u, _ := constant.Uint64Val(v.val)
		return uint32(u)
	case types.Uint64:
		u, _ := constant.Uint64Val(v.val)
		return u
	case types.Uintptr:
		u, _ := constant.Uint64Val(v.val)
		return uintptr(u)
	case types.Float32:
		f, _ := constant.Float32Val(v.val)
		return f
	case types.Float64:
		f, _ := constant.Float64Val(v.val)
		return f
	case types.Complex64:
		r, _ := constant.Float32Val(constant.Real(v.val))
		i, _ := constant.Float32Val(constant.Imag(v.val))
		return complex(r, i)
	case types.Complex128:
		r, _ := constant.Float64Val(constant.Real(v.val))
		i, _ := constant.Float64Val(constant.Imag(v.val))
		return complex(r, i)

	case types.UntypedBool:
		return constant.BoolVal(v.val)
	case types.UntypedInt:
		i, _ := constant.Int64Val(v.val)
		return i
	case types.UntypedRune:
		i, _ := constant.Int64Val(v.val)
		return rune(i)
	case types.UntypedFloat:
		f, _ := constant.Float64Val(v.val)
		return f
	case types.UntypedComplex:
		r, _ := constant.Float64Val(constant.Real(v.val))
		i, _ := constant.Float64Val(constant.Imag(v.val))
		return complex(r, i)
	case types.UntypedString:
		return constant.StringVal(v.val)
	case types.UntypedNil:
		return nil
	}
	panic("unsupported type")
}
