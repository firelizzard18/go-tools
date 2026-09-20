package testfuncs

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"
)

func TestEval(t *testing.T) {
	t.Run("Struct", func(t *testing.T) {
		type Case struct {
			X   any
			Src string
		}

		type B struct{ C string }
		typB := `type B struct{ C string }; `

		x1 := struct{ A int }{1}
		x2 := struct {
			A int
			B
		}{A: 1, B: B{C: "2"}}

		cases := []Case{
			{x1, `return struct{ A int }{1}`},
			{x1, `return struct{ A int }{A: 1}`},
			{x2, typB + `return struct{ A int; B }{1, B{"2"}}`},
			{x2, typB + `return struct{ A int; B }{A: 1, B: B{"2"}}`},
		}

		for i, c := range cases {
			t.Run(fmt.Sprint(i), func(t *testing.T) {
				typ, y := evalValue(t, c.Src)
				x := v2v(typ, c.X)
				if !reflect.DeepEqual(x, y) {
					t.Fatalf("want %v, got %v", x, y)
				}
			})
		}
	})

	t.Run("Collection", func(t *testing.T) {
		type Case struct {
			X   any
			Src string
		}

		x1 := []int{1, 0, 3}
		x2 := map[int]string{3: "foo"}
		cases := []Case{
			{x1, `return []int{1, 0, 3}`},
			{x1, `return [3]int{1, 0, 3}`},
			{x2, `return map[int]string{3: "foo"}`},
		}

		for i, c := range cases {
			t.Run(fmt.Sprint(i), func(t *testing.T) {
				typ, y := evalValue(t, c.Src)
				x := v2v(typ, c.X)
				if !reflect.DeepEqual(x, y) {
					t.Fatalf("want %v, got %v", x, y)
				}
			})
		}
	})
}

func evalValue(t testing.TB, src string) (types.Type, value) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "a.go", "package a; func A() any { "+src+" }", 0)
	if err != nil {
		t.Fatal(err)
	}

	x := &Context{Pass: &analysis.Pass{TypesInfo: &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:     make(map[ast.Node]*types.Scope),
	}}}
	x.Inspect = inspector.New([]*ast.File{f})

	cfg := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	_, err = cfg.Check("a", fset, []*ast.File{f}, x.TypesInfo)
	if err != nil {
		t.Fatal(err)
	}

	body := f.Decls[0].(*ast.FuncDecl).Body.List
	expr := body[len(body)-1].(*ast.ReturnStmt).Results[0]
	cur, ok := x.Inspect.Root().FindNode(expr)
	if !ok {
		t.Fatal("cursor not found")
	}

	typ := x.TypesInfo.TypeOf(expr)
	value, err := x.evaluate(expr, cur, nil)
	if err != nil {
		t.Fatal(err)
	}

	return typ, value
}

// v2v converts [any] to [value].
func v2v(typ types.Type, v any) value {
	rv, ok := v.(reflect.Value)
	if !ok {
		rv = reflect.ValueOf(v)
	}

	typ = typ.Underlying()
	switch rv.Kind() {
	case reflect.Bool, reflect.String:
		return constValue{constant.Make(rv.Interface())}

	case reflect.Int, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int8:
		return constValue{constant.MakeInt64(rv.Int())}

	case reflect.Uint, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint8:
		return constValue{constant.MakeUint64(rv.Uint())}

	case reflect.Array, reflect.Slice:
		typ := typ.(interface{ Elem() types.Type }).Elem()
		var v sliceValue
		for _, u := range rv.Seq2() {
			v = append(v, v2v(typ, u))
		}
		return v

	case reflect.Map:
		var v mapValue
		typ := typ.(*types.Map)
		for k, u := range rv.Seq2() {
			v = append(v, keyValuePair{v2v(typ.Key(), k), v2v(typ.Elem(), u)})
		}
		return v

	case reflect.Struct:
		v := structValue{}
		typ := typ.(*types.Struct)
		for i := range typ.NumFields() {
			f := typ.Field(i)
			v[f] = v2v(f.Type(), rv.Field(i))
		}
		return v

	case reflect.Pointer:
		typ := typ.(*types.Pointer).Elem()
		return v2v(typ, rv.Elem())

	default:
		panic("unsupported value")
	}
}
