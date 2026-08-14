// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package stats exercises the -stats flag: one record per test function,
// covering every status and every reason in the vocabulary.
//
// Both analyzer modes must agree on this package.
package stats

import "testing"

// table is package-level, so it has no single definite binding site.
var table = []struct{ name string }{{name: "a"}}

func helperName() string { return "x" }

func TestNoRuns(t *testing.T) { // want "TestNoRuns: runs=0 subtests=0 status=complete"
	t.Log("nothing to see here")
}

func TestConst(t *testing.T) { // want "TestConst: runs=2 subtests=2 status=complete"
	t.Run("a", func(t *testing.T) {})
	t.Run("b", func(t *testing.T) {})
}

func TestNested(t *testing.T) { // want "TestNested: runs=2 subtests=2 status=complete"
	t.Run("a", func(t *testing.T) {
		t.Run("b", func(t *testing.T) {})
	})
}

func TestTable(t *testing.T) { // want "TestTable: runs=1 subtests=3 status=complete"
	tcs := []struct{ name string }{{name: "a"}, {name: "b"}, {name: "c"}}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {})
	}
}

func TestPartial(t *testing.T) { // want "TestPartial: runs=2 subtests=1 status=partial reason=unsupported"
	t.Run("a", func(t *testing.T) {
		if testing.Short() {
			t.Run("b", func(t *testing.T) {})
		}
	})
}

func TestMutable(t *testing.T) { // want "TestMutable: runs=1 subtests=0 status=none reason=mutable"
	tcs := []struct{ name string }{{name: "a"}}
	tcs[0].name = "b"
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {})
	}
}

func TestNoBinding(t *testing.T) { // want "TestNoBinding: runs=1 subtests=0 status=none reason=no-binding"
	for _, tc := range table {
		t.Run(tc.name, func(t *testing.T) {})
	}
}

func TestDynamic(t *testing.T) { // want "TestDynamic: runs=1 subtests=0 status=none reason=dynamic"
	t.Run(helperName(), func(t *testing.T) {})
}

func TestInterprocedural(t *testing.T) { // want "TestInterprocedural: runs=1 subtests=0 status=none reason=interprocedural"
	call := func(f func()) { f() }
	call(func() {
		t.Run("a", func(t *testing.T) {})
	})
}

func BenchmarkRun(b *testing.B) { // want "BenchmarkRun: runs=1 subtests=1 status=complete"
	b.Run("a", func(b *testing.B) {})
}

func FuzzTarget(f *testing.F) { // want "FuzzTarget: runs=0 subtests=0 status=complete"
	f.Fuzz(func(t *testing.T, s string) {})
}

func ExampleTarget() { // want "ExampleTarget: runs=0 subtests=0 status=complete"
	// Output:
}
