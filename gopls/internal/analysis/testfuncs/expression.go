package testfuncs

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"iter"
)

type (
	Expression interface {
				Needs() iter.Seq[*ast.Ident]
		Bind(map[*ast.Ident]Expression) Expression
	}

	FuncExpr interface {
		Expression
		Func() (*ast.FuncType, *ast.BlockStmt)
	}

	Const struct {
		typ *types.Basic
		val constant.Value
	}

	Ident struct {
		*ast.Ident
	}

	FuncDecl struct {
		*ast.FuncDecl
	}

	FuncLit struct {
		*ast.FuncLit
	}

	Test struct {
		prefix   string
		name, fn Expression
		pos      token.Pos
	}
)

func (x *Context) exprFor(node ast.Node) (Expression, bool) {
	var expr ast.Expr
	switch node := node.(type) {
	case *ast.ExprStmt:
		return x.exprFor(node)
	case *ast.FuncDecl:
				return &FuncDecl{node}, true
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

	switch expr := expr.(type) {
	case *ast.Ident:
		obj := x.TypesInfo.ObjectOf(expr)
		if obj == nil {
			x.Reportf(expr.Pos(), "Unable to resolve identifier")
			return nil, false
		}
		return &Ident{expr}, true

	case *ast.FuncLit:
				return &FuncLit{expr}, true
	}

	x.Reportf(expr.Pos(), "Unable to resolve %T", expr)
	return nil, false
}

func (v *Ident) Needs() iter.Seq[*ast.Ident] {
	return func(yield func(*ast.Ident) bool) { yield(v.Ident) }
}

func (v *Ident) Bind(values map[*ast.Ident]Expression) Expression {
	if u, ok := values[v.Ident]; ok {
		return u
	}
	return v
}

func (v *Const) Type() types.Type                          { return v.typ }
func (v *Const) Needs() iter.Seq[*ast.Ident]               { return none }
func (v *Const) Bind(map[*ast.Ident]Expression) Expression { return v }

func (v *FuncLit) Needs() iter.Seq[*ast.Ident]               { return none }
func (v *FuncLit) Bind(map[*ast.Ident]Expression) Expression { return v }
func (v *FuncLit) Func() (*ast.FuncType, *ast.BlockStmt)     { return v.Type, v.Body }

func (v *FuncDecl) Needs() iter.Seq[*ast.Ident]               { return none }
func (v *FuncDecl) Bind(map[*ast.Ident]Expression) Expression { return v }
func (v *FuncDecl) Func() (*ast.FuncType, *ast.BlockStmt)     { return v.Type, v.Body }

func (v *Test) Type() types.Type { return types.Typ[types.Invalid] }

func (v *Test) Needs() iter.Seq[*ast.Ident] {
	return func(yield func(*ast.Ident) bool) {
		_ = yieldAll(v.name.Needs(), yield) &&
			yieldAll(v.fn.Needs(), yield)
	}
}

func (v *Test) Bind(values map[*ast.Ident]Expression) Expression {
	return &Test{
		prefix: v.prefix,
		name:   v.name.Bind(values),
		fn:     v.fn.Bind(values),
		pos:    v.pos,
	}
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

func none[V any](func(V) bool) {}

type Sprintf struct {
	format Expression
	args   []Expression
}

func (v *Sprintf) Type() types.Type { return types.Typ[types.String] }

func (v *Sprintf) Needs() iter.Seq[*ast.Ident] {
	return func(yield func(*ast.Ident) bool) {
		if !yieldAll(v.format.Needs(), yield) {
			return
		}
		for _, f := range v.args {
			if !yieldAll(f.Needs(), yield) {
				return
			}
		}
	}
}

func (v *Sprintf) Bind(values map[*ast.Ident]Expression) Expression {
	u := &Sprintf{
		format: v.format.Bind(values),
		args:   bindAll(v.args, values),
	}

	format, ok := u.format.(*Const)
	if !ok {
		return u
	}

	args := make([]any, len(u.args))
	for i, arg := range u.args {
		arg, ok := arg.(*Const)
		if !ok {
			return u
		}
		args[i] = arg.Value()
	}

	s := fmt.Sprintf(format.Value().(string), args...)
	return &Const{
		typ: types.Typ[types.String],
		val: constant.MakeString(s),
	}
}

func yieldAll[V any](it iter.Seq[V], yield func(V) bool) bool {
	for v := range it {
		if !yield(v) {
			return false
		}
	}
	return true
}

func bindAll(in []Expression, values map[*ast.Ident]Expression) []Expression {
	out := make([]Expression, len(in))
	for i, v := range in {
		out[i] = v.Bind(values)
	}
	return out
}

type Struct struct {
	typ    types.Type
	fields []Expression
}

func (v *Struct) Type() types.Type { return v.typ }

func (v *Struct) Needs() iter.Seq[*ast.Ident] {
	return func(yield func(*ast.Ident) bool) {
		for _, f := range v.fields {
			if !yieldAll(f.Needs(), yield) {
				return
			}
		}
	}
}

func (v *Struct) Bind(values map[*ast.Ident]Expression) Expression {
	return &Struct{
		typ:    v.typ,
		fields: bindAll(v.fields, values),
	}
}
