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

// A non-constant subtest name taints the test.
func TestNonConstName(t *testing.T) { // want `{"Name":"TestNonConstName","Tainted":"cannot determine subtest name"}`
	name := "sub"
	t.Run(name, func(t *testing.T) {})
}

func TestUnnamedParam(*testing.T) { // want `{"Name":"TestUnnamedParam"}`
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
