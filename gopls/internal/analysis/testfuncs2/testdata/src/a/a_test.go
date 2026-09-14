package a

import (
	"fmt"
	"testing"
)

func TestSimple(t *testing.T) { // want "Found: TestSimple"
	t.Run("sub", func(t *testing.T) {}) // want "Found: TestSimple/sub"
}

func TestNested(t *testing.T) { // want "Found: TestNested"
	t.Run("outer", func(t *testing.T) { // want "Found: TestNested/outer"
		t.Run("inner", func(t *testing.T) {}) // want "Found: TestNested/outer/inner"
	})
}

// Subtests with colliding names are not reported.
func TestDuplicate(t *testing.T) { // want "Found: TestDuplicate"
	t.Run("dup", func(t *testing.T) {})
	t.Run("dup", func(t *testing.T) {})
	t.Run("uniq", func(t *testing.T) {}) // want "Found: TestDuplicate/uniq"
}

// Non-Run calls on the TB don't taint it.
func TestLog(t *testing.T) { // want "Found: TestLog"
	t.Log("hi")
	t.Run("sub", func(t *testing.T) {}) // want "Found: TestLog/sub"
}

// Passing the TB to another function taints it.
func TestTainted(t *testing.T) { // want "Found: TestTainted" "Tainted \\(can't report children\\): TestTainted"
	helper(t)
	t.Run("sub", func(t *testing.T) {})
}

// A non-constant subtest name taints the test.
func TestNonConstName(t *testing.T) { // want "Found: TestNonConstName" "Tainted \\(can't report children\\): TestNonConstName"
	name := "sub"
	t.Run(name, func(t *testing.T) {})
}

func TestUnnamedParam(*testing.T) { // want "Found: TestUnnamedParam"
}

func BenchmarkSub(b *testing.B) { // want "Found: BenchmarkSub"
	b.Run("sub", func(b *testing.B) {}) // want "Found: BenchmarkSub/sub"
}

func BenchmarkRunParallel(b *testing.B) { // want "Found: BenchmarkRunParallel" "Tainted \\(can't report children\\): BenchmarkRunParallel"
	b.RunParallel(func(pb *testing.PB) {})
}

func FuzzFoo(f *testing.F) { // want "Found: FuzzFoo"
	f.Add(1)
	f.Fuzz(func(t *testing.T, x int) {
		t.Log(x)
	})
}

func ExampleFoo() { // want "Found: ExampleFoo"
	fmt.Println("Hi")
	// Output: Hi
}

func helper(t *testing.T) { t.Log("helper") }
