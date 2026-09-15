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

// Subtests with colliding names are not reported.
func TestDuplicate(t *testing.T) { // want `{"Name":"TestDuplicate"}`
	t.Run("dup", func(t *testing.T) {})
	t.Run("dup", func(t *testing.T) {})
	t.Run("uniq", func(t *testing.T) {}) // want `{"Name":"TestDuplicate/uniq"}`
}

// Non-Run calls on the TB don't taint it.
func TestLog(t *testing.T) { // want `{"Name":"TestLog"}`
	t.Log("hi")
	t.Run("sub", func(t *testing.T) {}) // want `{"Name":"TestLog/sub"}`
}

// Passing the TB to another function taints it.
func TestTainted(t *testing.T) { // want `{"Name":"TestTainted","Tainted":"non-call reference"}`
	helper(t)
	t.Run("sub", func(t *testing.T) {})
}

func TestUnnamedParam(*testing.T) { // want `{"Name":"TestUnnamedParam"}`
}

// A variable resolves if it is a local, declared in the function that reads it,
// written exactly once, unconditionally, before the read.

func TestVarDefine(t *testing.T) { // want `{"Name":"TestVarDefine"}`
	name := "sub"
	t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarDefine/sub"}`
}

func TestVarDeclThenAssign(t *testing.T) { // want `{"Name":"TestVarDeclThenAssign"}`
	var name string
	name = "sub"
	t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarDeclThenAssign/sub"}`
}

// The declaring function is the innermost one, not the test function.
func TestVarInCallback(t *testing.T) { // want `{"Name":"TestVarInCallback"}`
	t.Run("outer", func(t *testing.T) { // want `{"Name":"TestVarInCallback/outer"}`
		name := "sub"
		t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarInCallback/outer/sub"}`
	})
}

// An inner declaration shadows an outer one; the inner object is the one
// resolved, and the outer variable's mentions do not count against it.
func TestVarShadowed(t *testing.T) { // want `{"Name":"TestVarShadowed"}`
	name := "outer"
	_ = name
	t.Run("outer", func(t *testing.T) { // want `{"Name":"TestVarShadowed/outer"}`
		name := "sub"
		t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarShadowed/outer/sub"}`
	})
}

// Resolution recurses through the bound expression.
func TestVarChained(t *testing.T) { // want `{"Name":"TestVarChained"}`
	a := "sub"
	b := a
	t.Run(b, func(t *testing.T) {}) // want `{"Name":"TestVarChained/sub"}`
}

func TestVarStructField(t *testing.T) { // want `{"Name":"TestVarStructField"}`
	tc := testCase{name: "sub"}
	t.Run(tc.name, func(t *testing.T) {}) // want `{"Name":"TestVarStructField/sub"}`
}

// Compound assignment is not a binding: the prior value participates, and the
// prior value is not being read. It happens to be the identity here, because
// += on "" is, but *= on an int is not.
func TestVarCompoundAssign(t *testing.T) { // want `{"Name":"TestVarCompoundAssign","Tainted":"cannot determine subtest name`
	var name string
	name += "sub"
	t.Run(name, func(t *testing.T) {})
}

// A second write disqualifies the variable.
func TestVarReassigned(t *testing.T) { // want `{"Name":"TestVarReassigned","Tainted":"cannot determine subtest name`
	name := "sub"
	name = "foo"
	t.Run(name, func(t *testing.T) {})
}

// A conditional write is not a binding we can trust.
func TestVarConditionalWrite(t *testing.T) { // want `{"Name":"TestVarConditionalWrite","Tainted":"cannot determine subtest name`
	var name string
	if false {
		name = "foo"
	}
	t.Run(name, func(t *testing.T) {})
}

// A write inside a nested closure may never run, or may run after the read.
func TestVarClosureWrite(t *testing.T) { // want `{"Name":"TestVarClosureWrite","Tainted":"cannot determine subtest name`
	var name string
	_ = func() { name = "foo" }
	t.Run(name, func(t *testing.T) {})
}

// The variable must be declared in the function that reads it, otherwise
// writes outside that function are invisible.
func TestVarDeclaredOutside(t *testing.T) { // want `{"Name":"TestVarDeclaredOutside"}`
	var name string
	t.Run("outer", func(t *testing.T) { // want `{"Name":"TestVarDeclaredOutside/outer","Tainted":"cannot determine subtest name`
		name = "sub"
		t.Run(name, func(t *testing.T) {})
	})
}

// A read and a write are not syntactically distinguishable, so the mention
// count is the mutability check. A harmless third mention disqualifies the
// variable. This is deliberate; it is the price of never reporting a name that
// the test does not run.
func TestVarExtraMention(t *testing.T) { // want `{"Name":"TestVarExtraMention","Tainted":"cannot determine subtest name`
	name := "sub"
	t.Log(name)
	t.Run(name, func(t *testing.T) {})
}

// Mutating a member counts as a mention of the composite.
func TestVarStructFieldWrite(t *testing.T) { // want `{"Name":"TestVarStructFieldWrite","Tainted":"cannot determine subtest name`
	tc := testCase{name: "sub"}
	tc.name = "foo"
	t.Run(tc.name, func(t *testing.T) {})
}

func TestVarWriteAfterRead(t *testing.T) { // want `{"Name":"TestVarWriteAfterRead","Tainted":"cannot determine subtest name`
	var name string
	t.Run(name, func(t *testing.T) {})
	name = "foo"
}

// A package-level variable's declaration says nothing about its value at the
// time the test runs.
func TestVarPackageLevel(t *testing.T) { // want `{"Name":"TestVarPackageLevel","Tainted":"cannot determine subtest name`
	t.Run(pkgName, func(t *testing.T) {})
}

func TestVarMultiValue(t *testing.T) { // want `{"Name":"TestVarMultiValue","Tainted":"cannot determine subtest name`
	name, _ := twoStrings()
	t.Run(name, func(t *testing.T) {})
}

func TestVarDeclWithValue(t *testing.T) { // want `{"Name":"TestVarDeclWithValue"}`
	var name = "sub"
	t.Run(name, func(t *testing.T) {}) // want `{"Name":"TestVarDeclWithValue/sub"}`
}

// A read is not a write, even though its parent is an AssignStmt. The index of
// the ident within Lhs must not be used to index Rhs.
func TestVarReadOnAssignRhs(t *testing.T) { // want `{"Name":"TestVarReadOnAssignRhs","Tainted":"cannot determine subtest name`
	var name string
	_ = name
	t.Run(name, func(t *testing.T) {})
}

// Likewise for a ValueSpec that merely mentions the variable in its values;
// such a spec declares y, not name.
func TestVarReadOnSpecValue(t *testing.T) { // want `{"Name":"TestVarReadOnSpecValue","Tainted":"cannot determine subtest name`
	var name string
	var y = name
	_ = y
	t.Run(name, func(t *testing.T) {})
}

func BenchmarkSub(b *testing.B) { // want `{"Name":"BenchmarkSub"}`
	b.Run("sub", func(b *testing.B) {}) // want `{"Name":"BenchmarkSub/sub"}`
}

func BenchmarkRunParallel(b *testing.B) { // want `{"Name":"BenchmarkRunParallel","Tainted":"RunParallel is not supported"}`
	b.RunParallel(func(pb *testing.PB) {})
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

func helper(t *testing.T) { t.Log("helper") }

type testCase struct{ name string }

var pkgName = "sub"

func twoStrings() (string, string) { return "sub", "other" }
