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
		Type() types.Type
		Needs() iter.Seq[ast.Node]
		Bind(map[ast.Node]Expression) Expression
	}

	FuncExpr interface {
		Expression
		Func() (*ast.FuncType, *ast.BlockStmt)
	}

	Const struct {
		typ *types.Basic
		val constant.Value
	}

	FuncDecl struct {
		typ *types.Signature
		val *ast.FuncDecl
	}

	FuncLit struct {
		typ *types.Signature
		val *ast.FuncLit
	}

	Test struct {
		prefix   string
		name, fn Expression
		pos      token.Pos
	}
)

func (x *Context) exprForExpr(expr ast.Expr) (Expression, bool) {
	// Is the value a constant?
	if tv, ok := x.TypesInfo.Types[expr]; ok && tv.Value != nil {
		if typ, ok := tv.Type.(*types.Basic); ok {
			return &Const{typ, tv.Value}, true
		}
	}

	switch expr := expr.(type) {
	case *ast.FuncLit:
		sig := x.TypesInfo.TypeOf(expr).(*types.Signature)
		return &FuncLit{typ: sig, val: expr}, true
	}

	x.Reportf(expr.Pos(), "Unable to resolve expression")
	return nil, false
}

func (v *Const) Type() types.Type                        { return v.typ }
func (v *Const) Needs() iter.Seq[ast.Node]               { return none }
func (v *Const) Bind(map[ast.Node]Expression) Expression { return v }

func (v *FuncLit) Type() types.Type                        { return v.typ }
func (v *FuncLit) Needs() iter.Seq[ast.Node]               { return none }
func (v *FuncLit) Bind(map[ast.Node]Expression) Expression { return v }
func (v *FuncLit) Func() (*ast.FuncType, *ast.BlockStmt)   { return v.val.Type, v.val.Body }

func (v *FuncDecl) Type() types.Type                        { return v.typ }
func (v *FuncDecl) Needs() iter.Seq[ast.Node]               { return none }
func (v *FuncDecl) Bind(map[ast.Node]Expression) Expression { return v }
func (v *FuncDecl) Func() (*ast.FuncType, *ast.BlockStmt)   { return v.val.Type, v.val.Body }

func (v *Test) Type() types.Type { return types.Typ[types.Invalid] }

func (v *Test) Needs() iter.Seq[ast.Node] {
	return func(yield func(ast.Node) bool) {
		_ = yieldAll(v.name.Needs(), yield) &&
			yieldAll(v.fn.Needs(), yield)
	}
}

func (v *Test) Bind(values map[ast.Node]Expression) Expression {
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

func (v *Sprintf) Needs() iter.Seq[ast.Node] {
	return func(yield func(ast.Node) bool) {
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

func (v *Sprintf) Bind(values map[ast.Node]Expression) Expression {
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

func bindAll(in []Expression, values map[ast.Node]Expression) []Expression {
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

func (v *Struct) Needs() iter.Seq[ast.Node] {
	return func(yield func(ast.Node) bool) {
		for _, f := range v.fields {
			if !yieldAll(f.Needs(), yield) {
				return
			}
		}
	}
}

func (v *Struct) Bind(values map[ast.Node]Expression) Expression {
	return &Struct{
		typ:    v.typ,
		fields: bindAll(v.fields, values),
	}
}
