package a

import (
	"fmt"
	"testing"
)

func TestSimple(t *testing.T) { // want `{"Name":"TestSimple"}`
	t.Run("sub", func(t *testing.T) {}) // want `{"Name":"TestSimple/sub"}`
}

func TestNested(t *testing.T) { // want `{"Name":"TestNested"}`
	t.Run("outer", func(t *testing.T) { // want `{"Name":"TestNested/outer"}`
		t.Run("inner", func(t *testing.T) {}) // want `{"Name":"TestNested/outer/inner"}`
	})
}

func TestDuplicate(t *testing.T) { // want `{"Name":"TestDuplicate"}`
	t.Run("dup", func(t *testing.T) {})  // want `{"Name":"TestDuplicate/dup"}`
	t.Run("dup", func(t *testing.T) {})  // want `{"Name":"TestDuplicate/dup#01"}`
	t.Run("uniq", func(t *testing.T) {}) // want `{"Name":"TestDuplicate/uniq"}`
}

func TestLog(t *testing.T) { // want `{"Name":"TestLog"}`
	t.Log("hi")
	t.Run("sub", func(t *testing.T) {}) // want `{"Name":"TestLog/sub"}`
}

func TestBadHelper(t *testing.T) { // want `{"Name":"TestBadHelper"}`
	badHelper(t) // want `{"Name":"TestBadHelper","Error":"escapes"`
	t.Run("sub", func(t *testing.T) {})
}

func TestGoodHelper(t *testing.T) { // want `{"Name":"TestGoodHelper"}`
	goodHelper(t)
	t.Run("sub", func(t *testing.T) {}) // want `{"Name":"TestGoodHelper/sub"}`
}

func TestLogHelper(t *testing.T) { // want `{"Name":"TestLogHelper"}`
	logHelper(t)
	t.Run("sub", func(t *testing.T) {}) // want `{"Name":"TestLogHelper/sub"}`
}

func TestUnnamedParam(*testing.T) { // want `{"Name":"TestUnnamedParam"}`
}

func TestVarDefine(t *testing.T) { // want `{"Name":"TestVarDefine"}`
	name := "sub"
	t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarDefine/sub"}`
}

func TestVarDeclThenAssign(t *testing.T) { // want `{"Name":"TestVarDeclThenAssign"}`
	var name string
	name = "sub"
	t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarDeclThenAssign/sub"}`
}

func TestVarInCallback(t *testing.T) { // want `{"Name":"TestVarInCallback"}`
	t.Run("outer", func(t *testing.T) { // want `{"Name":"TestVarInCallback/outer"}`
		name := "sub"
		t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarInCallback/outer/sub"}`
	})
}

func TestVarShadowed(t *testing.T) { // want `{"Name":"TestVarShadowed"}`
	name := "outer"
	_ = name
	t.Run("outer", func(t *testing.T) { // want `{"Name":"TestVarShadowed/outer"}`
		name := "sub"
		t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarShadowed/outer/sub"}`
	})
}

func TestVarChained(t *testing.T) { // want `{"Name":"TestVarChained"}`
	a := "sub"
	b := a // want `{"Name":"TestVarChained","Error":"unresolved"`
	t.Run(b, func(t *testing.T) {})
}

func TestVarStructField(t *testing.T) { // want `{"Name":"TestVarStructField"}`
	tc := testCase{name: "sub"}
	t.Run(tc.name, func(t *testing.T) {}) // want `{"Name":"TestVarStructField/sub"}`
}

func TestVarCompoundAssign(t *testing.T) { // want `{"Name":"TestVarCompoundAssign"}`
	var name string
	name += "sub"
	t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarCompoundAssign","Error":"unresolved"`
}

func TestVarReassigned(t *testing.T) { // want `{"Name":"TestVarReassigned"}`
	name := "sub"
	name = "foo" // want `{"Name":"TestVarReassigned","Error":"unresolved"`
	t.Run(name, func(t *testing.T) {})
}

func TestVarConditionalWrite(t *testing.T) { // want `{"Name":"TestVarConditionalWrite"}`
	var name string
	if false { // want `{"Name":"TestVarConditionalWrite","Error":"unmodeled"`
		name = "foo"
	}
	t.Run(name, func(t *testing.T) {})
}

func TestVarClosureWrite(t *testing.T) { // want `{"Name":"TestVarClosureWrite"}`
	var name string
	_ = func() { name = "foo" }
	t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarClosureWrite","Error":"unresolved"`
}

func TestVarDeclaredOutside(t *testing.T) { // want `{"Name":"TestVarDeclaredOutside"}`
	var name string
	t.Run("outer", func(t *testing.T) { // want `{"Name":"TestVarDeclaredOutside/outer"}`
		name = "sub"
		t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarDeclaredOutside/outer","Error":"unresolved"`
	})
}

func TestVarExtraMention(t *testing.T) { // want `{"Name":"TestVarExtraMention"}`
	name := "sub"
	t.Log(name)
	t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarExtraMention/sub"}`
}

func TestVarStructFieldWrite(t *testing.T) { // want `{"Name":"TestVarStructFieldWrite"}`
	tc := testCase{name: "sub"}
	tc.name = "foo" // want `{"Name":"TestVarStructFieldWrite","Error":"unresolved"`
	t.Run(tc.name, func(t *testing.T) {})
}

func TestVarWriteAfterRead(t *testing.T) { // want `{"Name":"TestVarWriteAfterRead"}`
	var name string
	t.Run(name, func(t *testing.T) {})
	name = "foo" // want `{"Name":"TestVarWriteAfterRead","Error":"unresolved"`
}

func TestVarPackageLevel(t *testing.T) { // want `{"Name":"TestVarPackageLevel"}`
	t.Run(pkgName, func(t *testing.T) {}) // want `{"Name":"TestVarPackageLevel","Error":"unmodeled"`
}

func TestVarMultiValue(t *testing.T) { // want `{"Name":"TestVarMultiValue"}`
	name, _ := twoStrings()
	t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarMultiValue","Error":"unresolved"`
}

func TestVarDeclWithValue(t *testing.T) { // want `{"Name":"TestVarDeclWithValue"}`
	var name = "sub"
	t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarDeclWithValue/sub"}`
}

func TestVarReadOnAssignRhs(t *testing.T) { // want `{"Name":"TestVarReadOnAssignRhs"}`
	var name string
	_ = name // want `{"Name":"TestVarReadOnAssignRhs","Error":"unresolved"`
	t.Run(name, func(t *testing.T) {})
}

func TestPackageFuncCallback(t *testing.T) { // want `{"Name":"TestPackageFuncCallback"}`
	t.Run("sub", subtest) // want `{"Name":"TestPackageFuncCallback/sub"}`
}

func BenchmarkSub(b *testing.B) { // want `{"Name":"BenchmarkSub"}`
	b.Run("sub", func(b *testing.B) {}) // want `{"Name":"BenchmarkSub/sub"}`
}

func FuzzFoo(f *testing.F) { // want `{"Name":"FuzzFoo"}`
	f.Add(1)
	f.Fuzz(func(t *testing.T, x int) {
		t.Log(x)
	})
}

func ExampleFoo() { // want `{"Name":"ExampleFoo"}`
	fmt.Println("Hi")
	// Output: Hi
}

func TestGenericParam(t *testing.T) { // want `{"Name":"TestGenericParam"}`
	// Generics are a bit weird. A parameter might _not_ be runnable when the
	// indexer checks it, and then might become runnable when it's instantiated.
	genericHelper(t, t) // want `{"Name":"TestGenericParam","Error":"escapes"`
	t.Run("sub", func(t *testing.T) {})
}

func genericHelper[T any](a T, tb *testing.T) {}

var badHelper func(*testing.T)
var goodHelper func(testing.TB)

func logHelper(t testing.TB) {
	t.Log("helper")
}

func subtest(t *testing.T) {
	t.Run("inner", func(t *testing.T) {}) // want `{"Name":"TestPackageFuncCallback/sub/inner"}`
}

type testCase struct{ name string }

var pkgName = "sub"

func twoStrings() (string, string) { return "sub", "other" }
