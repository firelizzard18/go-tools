// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testfuncs

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
)

// resolution describes how completely an [Expression] was evaluated.
//
// The values are ordered from least to most resolved so that the resolution of
// a compound expression is the [min] of the resolutions of its operands.
type resolution int

const (
	// unresolvable indicates the expression cannot be evaluated statically,
	// now or ever. The caller must not report anything derived from it.
	unresolvable resolution = iota

	// deferred indicates the expression depends on a range variable that is
	// not currently bound. Evaluating it again once [Context.Values] binds
	// that variable may produce a resolved value.
	deferred

	// resolved indicates the expression was fully evaluated.
	resolved
)

type (
	// Expression is a partially interpreted Go expression.
	//
	// Eval returns the most resolved form of the expression that the current
	// [Context] permits, plus how resolved that form is. An Expression that
	// returns [deferred] must return a value that can usefully be evaluated
	// again later; see [Context.Values].
	Expression interface {
		Eval(*Context) (Expression, resolution)
	}

	// Const is a constant value.
	Const struct {
		typ *types.Basic
		val constant.Value
	}

	// Ident is a reference to an object, resolved via [Context.resolve].
	Ident struct {
		*ast.Ident
	}

	// Field is a struct field selection, x.name.
	Field struct {
		x    Expression
		name string
	}

	// Index is an index expression, x[i].
	Index struct {
		x Expression
		i Expression
	}

	// Struct is a struct value. A nil element denotes a field whose value is
	// unknown.
	Struct struct {
		typ    *types.Struct
		fields []Expression
	}

	// Slice is a slice or array value. A nil element denotes an element whose
	// value is unknown.
	Slice []Expression

	// Map is a map value. Keys are always known (a map with an unknown key is
	// unresolvable, since we could not enumerate it); a nil value denotes an
	// entry whose value is unknown.
	Map struct {
		keys []Expression
		vals []Expression
	}

	// FuncExpr is a function literal or declaration.
	FuncExpr struct {
		typ  *ast.FuncType
		body *ast.BlockStmt
	}

	// Sprintf is a call to [fmt.Sprintf], or something that can be modeled as
	// one (such as [strconv.Itoa]).
	Sprintf struct {
		format Expression
		args   []Expression
	}

	// Binary is a binary expression. Only token.ADD is modeled, which covers
	// string concatenation.
	Binary struct {
		op   token.Token
		x, y Expression
	}
)

// exprFor builds an [Expression] for the given syntax node, eagerly evaluating
// it as far as the current [Context] allows.
func (x *Context) exprFor(node ast.Node) (Expression, resolution) {
	var expr ast.Expr
	switch node := node.(type) {
	case *ast.ExprStmt:
		expr = node.X
	case *ast.FuncDecl:
		if node.Body == nil {
			return nil, unresolvable
		}
		return &FuncExpr{node.Type, node.Body}, resolved
	case ast.Expr:
		expr = node
	default:
		x.debugf(reasonUnsupported, node.Pos(), "Unable to resolve %T", node)
		return nil, unresolvable
	}

	// Is the value a constant? This covers untyped constant folding, including
	// concatenation of string literals and constants.
	if tv, ok := x.TypesInfo.Types[expr]; ok && tv.Value != nil {
		if typ, ok := tv.Type.(*types.Basic); ok {
			return &Const{typ, tv.Value}, resolved
		}
	}

	var val Expression
	switch expr := expr.(type) {
	case *ast.ParenExpr:
		return x.exprFor(expr.X)

	case *ast.FuncLit:
		val = &FuncExpr{expr.Type, expr.Body}

	case *ast.Ident:
		val = &Ident{expr}

	case *ast.SelectorExpr:
		// A qualified identifier (pkg.Name) is a reference to an object, not a
		// field selection.
		if sel, ok := x.TypesInfo.Selections[expr]; !ok || sel.Kind() != types.FieldVal {
			val = &Ident{expr.Sel}
			break
		}

		y, r := x.exprFor(expr.X)
		if r == unresolvable {
			return nil, unresolvable
		}
		val = &Field{y, expr.Sel.Name}

	case *ast.IndexExpr:
		y, r1 := x.exprFor(expr.X)
		i, r2 := x.exprFor(expr.Index)
		if min(r1, r2) == unresolvable {
			return nil, unresolvable
		}
		val = &Index{y, i}

	case *ast.BinaryExpr:
		if expr.Op != token.ADD {
			x.debugf(reasonUnsupported, expr.Pos(), "Unable to resolve binary operator %v", expr.Op)
			return nil, unresolvable
		}
		y, r1 := x.exprFor(expr.X)
		z, r2 := x.exprFor(expr.Y)
		if min(r1, r2) == unresolvable {
			return nil, unresolvable
		}
		val = &Binary{expr.Op, y, z}

	case *ast.CompositeLit:
		return x.compositeExprFor(expr)

	case *ast.CallExpr:
		return x.callExprFor(expr)

	default:
		x.debugf(reasonUnsupported, expr.Pos(), "Unable to resolve %T", expr)
		return nil, unresolvable
	}

	// Eagerly resolve identifiers, selectors, and operators.
	return val.Eval(x)
}

// compositeExprFor builds an [Expression] for a composite literal.
func (x *Context) compositeExprFor(lit *ast.CompositeLit) (Expression, resolution) {
	typ := x.TypesInfo.TypeOf(lit)
	if typ == nil {
		return nil, unresolvable
	}

	switch typ := typ.Underlying().(type) {
	case *types.Struct:
		s := &Struct{
			typ:    typ,
			fields: make([]Expression, typ.NumFields()),
		}

		for i, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				key, ok := kv.Key.(*ast.Ident)
				if !ok {
					x.debugf(reasonUnsupported, lit.Pos(), "Unable to resolve struct literal: %T is not a legal field name", kv.Key)
					return nil, unresolvable
				}

				i = findStructField(typ, key.Name)
				if i < 0 {
					x.debugf(reasonUnsupported, lit.Pos(), "Unable to resolve struct literal: %q is not a field of %v", key.Name, typ)
					return nil, unresolvable
				}

				elt = kv.Value
			}
			if i >= len(s.fields) {
				return nil, unresolvable // "can't happen"
			}

			// An unresolvable field is recorded as nil (unknown). That is not
			// fatal: the field may never be used to name a subtest.
			if v, r := x.exprFor(elt); r != unresolvable {
				s.fields[i] = v
			}
		}
		return s, resolved

	case *types.Slice, *types.Array:
		s := make(Slice, len(lit.Elts))
		for i, elt := range lit.Elts {
			// Keyed array/slice literals ({3: "x"}) are not modeled.
			if _, ok := elt.(*ast.KeyValueExpr); ok {
				x.debugf(reasonUnsupported, lit.Pos(), "Unable to resolve keyed slice literal")
				return nil, unresolvable
			}
			if v, r := x.exprFor(elt); r != unresolvable {
				s[i] = v
			}
		}
		return s, resolved

	case *types.Map:
		m := &Map{}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				return nil, unresolvable // "can't happen"
			}

			// Unlike a struct field or slice element, an unknown key makes the
			// whole map unusable: we could not enumerate its subtests, and a
			// missing subtest would corrupt the "#NN" suffixes of the others.
			k, r := x.exprFor(kv.Key)
			if r != resolved {
				x.debugf(reasonDynamic, kv.Key.Pos(), "Unable to resolve map key")
				return nil, unresolvable
			}

			v, r := x.exprFor(kv.Value)
			if r == unresolvable {
				v = nil
			}
			m.keys = append(m.keys, k)
			m.vals = append(m.vals, v)
		}
		return m, resolved

	default:
		x.debugf(reasonUnsupported, lit.Pos(), "Unable to resolve composite literal: %v not supported", typ)
		return nil, unresolvable
	}
}

// callExprFor builds an [Expression] for the handful of calls that produce
// statically predictable strings. Everything else is unresolvable; recursing
// into arbitrary functions is out of scope.
func (x *Context) callExprFor(call *ast.CallExpr) (Expression, resolution) {
	if call.Ellipsis.IsValid() {
		x.debugf(reasonDynamic, call.Pos(), "Unable to resolve call with ... argument")
		return nil, unresolvable
	}

	fn, ok := x.calleeFunc(call)
	if !ok || fn.Pkg() == nil {
		x.debugf(reasonDynamic, call.Pos(), "Unable to resolve call")
		return nil, unresolvable
	}

	var format Expression
	var args []ast.Expr
	switch {
	case fn.Pkg().Path() == "fmt" && fn.Name() == "Sprintf" && len(call.Args) > 0:
		f, r := x.exprFor(call.Args[0])
		if r == unresolvable {
			return nil, unresolvable
		}
		format, args = f, call.Args[1:]

	case fn.Pkg().Path() == "strconv" && fn.Name() == "Itoa" && len(call.Args) == 1:
		format, args = &Const{types.Typ[types.String], constant.MakeString("%d")}, call.Args

	default:
		x.debugf(reasonDynamic, call.Pos(), "Unable to resolve call to %v", fn.FullName())
		return nil, unresolvable
	}

	s := &Sprintf{format: format}
	for _, arg := range args {
		a, r := x.exprFor(arg)
		if r == unresolvable {
			return nil, unresolvable
		}
		s.args = append(s.args, a)
	}
	return s.Eval(x)
}

// calleeFunc returns the function or method that call invokes, if it is a
// static call to a declared function. It deliberately resolves the callee
// through the type checker rather than matching on package identifier names,
// so that renamed imports are handled correctly.
func (x *Context) calleeFunc(call *ast.CallExpr) (*types.Func, bool) {
	var id *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = fun
	case *ast.SelectorExpr:
		id = fun.Sel
	default:
		return nil, false
	}
	fn, ok := x.TypesInfo.Uses[id].(*types.Func)
	return fn, ok
}

func (v *Const) Eval(*Context) (Expression, resolution)    { return v, resolved }
func (v *FuncExpr) Eval(*Context) (Expression, resolution) { return v, resolved }

func (v *Ident) Eval(ctx *Context) (Expression, resolution) {
	return ctx.resolve(v.Ident)
}

func (v *Field) Eval(ctx *Context) (Expression, resolution) {
	y, r := v.x.Eval(ctx)
	if r != resolved {
		return &Field{y, v.name}, r
	}

	s, ok := y.(*Struct)
	if !ok {
		return nil, unresolvable // not a struct
	}
	i := findStructField(s.typ, v.name)
	if i < 0 || s.fields[i] == nil {
		return nil, unresolvable // no such field, or its value is unknown
	}
	return s.fields[i].Eval(ctx)
}

func (v *Index) Eval(ctx *Context) (Expression, resolution) {
	y, r1 := v.x.Eval(ctx)
	i, r2 := v.i.Eval(ctx)
	if r := min(r1, r2); r != resolved {
		return &Index{y, i}, r
	}

	idx, ok := i.(*Const)
	if !ok {
		return nil, unresolvable
	}

	switch y := y.(type) {
	case Slice:
		if idx.val.Kind() != constant.Int {
			return nil, unresolvable
		}
		n, ok := constant.Uint64Val(idx.val)
		if !ok || n >= uint64(len(y)) || y[n] == nil {
			return nil, unresolvable // out of bounds or unknown value
		}
		return y[n].Eval(ctx)

	case *Map:
		for i, k := range y.keys {
			k, ok := k.(*Const)
			if ok && constant.Compare(k.val, token.EQL, idx.val) {
				if y.vals[i] == nil {
					return nil, unresolvable
				}
				return y.vals[i].Eval(ctx)
			}
		}
		return nil, unresolvable // absent key yields the zero value; not modeled
	}
	return nil, unresolvable
}

func (v *Struct) Eval(ctx *Context) (Expression, resolution) {
	fields, r := evalAll(ctx, v.fields)
	return &Struct{typ: v.typ, fields: fields}, r
}

func (v Slice) Eval(ctx *Context) (Expression, resolution) {
	u, r := evalAll(ctx, v)
	return Slice(u), r
}

func (v *Map) Eval(ctx *Context) (Expression, resolution) {
	keys, r1 := evalAll(ctx, v.keys)
	vals, r2 := evalAll(ctx, v.vals)
	return &Map{keys: keys, vals: vals}, min(r1, r2)
}

func (v *Sprintf) Eval(ctx *Context) (Expression, resolution) {
	format, r := v.format.Eval(ctx)
	args, r2 := evalAll(ctx, v.args)
	r = min(r, r2)

	u := &Sprintf{format, args}
	if r != resolved {
		return u, r
	}

	fmtVal, ok := format.(*Const)
	if !ok {
		return nil, unresolvable
	}
	fmtStr, ok := fmtVal.Value()
	if _, isStr := fmtStr.(string); !ok || !isStr {
		return nil, unresolvable
	}

	argVals := make([]any, len(args))
	for i, arg := range args {
		arg, ok := arg.(*Const)
		if !ok {
			return nil, unresolvable
		}
		argVals[i], ok = arg.Value()
		if !ok {
			return nil, unresolvable
		}
	}

	s := fmt.Sprintf(fmtStr.(string), argVals...)

	// Sprintf reports malformed verbs and argument-count mismatches in-band.
	// Rather than reproduce its rules, detect its complaints and bail: a name
	// containing "%!" is almost certainly our mistake, not the test's.
	if strings.Contains(s, "%!") {
		return nil, unresolvable
	}

	return &Const{types.Typ[types.String], constant.MakeString(s)}, resolved
}

func (v *Binary) Eval(ctx *Context) (Expression, resolution) {
	y, r1 := v.x.Eval(ctx)
	z, r2 := v.y.Eval(ctx)
	if r := min(r1, r2); r != resolved {
		return &Binary{v.op, y, z}, r
	}

	cy, ok1 := y.(*Const)
	cz, ok2 := z.(*Const)
	if !ok1 || !ok2 || cy.val.Kind() != cz.val.Kind() {
		// constant.BinaryOp panics on mismatched kinds.
		return nil, unresolvable
	}
	if cy.val.Kind() == constant.Unknown {
		return nil, unresolvable
	}

	typ := cy.typ
	if typ.Info()&types.IsUntyped != 0 {
		typ = cz.typ
	}
	return &Const{typ, constant.BinaryOp(cy.val, v.op, cz.val)}, resolved
}

// Value converts a constant to the Go value fmt would format, reporting false
// if the type is not one this package models.
func (v *Const) Value() (any, bool) {
	switch v.typ.Kind() {
	case types.Bool, types.UntypedBool:
		return constant.BoolVal(v.val), true
	case types.String, types.UntypedString:
		return constant.StringVal(v.val), true
	case types.Int, types.UntypedInt:
		i, ok := constant.Int64Val(v.val)
		return int(i), ok
	case types.Int8:
		i, ok := constant.Int64Val(v.val)
		return int8(i), ok
	case types.Int16:
		i, ok := constant.Int64Val(v.val)
		return int16(i), ok
	case types.Int32, types.UntypedRune:
		i, ok := constant.Int64Val(v.val)
		return int32(i), ok
	case types.Int64:
		i, ok := constant.Int64Val(v.val)
		return i, ok
	case types.Uint:
		u, ok := constant.Uint64Val(v.val)
		return uint(u), ok
	case types.Uint8:
		u, ok := constant.Uint64Val(v.val)
		return uint8(u), ok
	case types.Uint16:
		u, ok := constant.Uint64Val(v.val)
		return uint16(u), ok
	case types.Uint32:
		u, ok := constant.Uint64Val(v.val)
		return uint32(u), ok
	case types.Uint64:
		u, ok := constant.Uint64Val(v.val)
		return u, ok
	case types.Uintptr:
		u, ok := constant.Uint64Val(v.val)
		return uintptr(u), ok
	case types.Float32:
		f, ok := constant.Float32Val(v.val)
		return f, ok
	case types.Float64, types.UntypedFloat:
		f, ok := constant.Float64Val(v.val)
		return f, ok
	}
	return nil, false
}

func findStructField(typ *types.Struct, name string) int {
	for i := range typ.NumFields() {
		if typ.Field(i).Name() == name {
			return i
		}
	}
	return -1
}
