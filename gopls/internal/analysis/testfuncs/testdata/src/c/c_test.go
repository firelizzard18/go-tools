// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package c is an adversarial regression suite. Every test in it is a case
// where a naive reading of the syntax produces a subtest name that the test
// never actually runs. The analyzer must either report the correct name or
// report nothing; it must never report a wrong one.
package c

import "testing"

type testCase struct{ name string }

// pkgCases is package-level state, so any function in the package may change
// it before the test that ranges over it runs.
var pkgCases = []testCase{{name: "pkg"}}

func mutatePkg() { pkgCases[0].name = "mutated" }

func mutate(s []testCase) { s[0].name = "mutated" }

// The element is overwritten after the literal, so "before" is never used.
func TestMutate(t *testing.T) { // want "Found: TestMutate"
	tcs := []testCase{{name: "before"}}
	tcs[0].name = "after"
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {})
	}
}

// The slice is aliased into a helper that writes through it, so "orig" is
// never used.
func TestAlias(t *testing.T) { // want "Found: TestAlias"
	tcs := []testCase{{name: "orig"}}
	mutate(tcs)
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {})
	}
}

// Only one of the two literals is ever used, and which one is not known
// statically.
func TestConditional(t *testing.T) { // want "Found: TestConditional"
	var tcs []testCase
	if testing.Short() {
		tcs = []testCase{{name: "short"}}
	} else {
		tcs = []testCase{{name: "long"}}
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {})
	}
}

// The slice is extended after it is created.
func TestAppend(t *testing.T) { // want "Found: TestAppend"
	tcs := []testCase{{name: "one"}}
	tcs = append(tcs, testCase{name: "two"})
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {})
	}
}

// mutatePkg may have run first, so "pkg" cannot be trusted.
func TestPackageLevel(t *testing.T) { // want "Found: TestPackageLevel"
	mutatePkg()
	for _, tc := range pkgCases {
		t.Run(tc.name, func(t *testing.T) {})
	}
}

// A subtest whose name cannot be determined consumes one of the "#NN"
// suffixes, so none of its siblings can be trusted either. The real names here
// are "dup", "dup#01", and "dup#02", but only if the middle one runs.
func TestUnresolvedSibling(t *testing.T) { // want "Found: TestUnresolvedSibling"
	t.Run("dup", func(t *testing.T) {})
	if testing.Short() {
		t.Run("dup", func(t *testing.T) {})
	}
	t.Run("dup", func(t *testing.T) {})
}

// The control case: nothing touches tcs, so both modes must resolve it.
func TestClean(t *testing.T) { // want "Found: TestClean"
	tcs := []testCase{
		{name: "one"},
		{name: "two"},
		{name: "one"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {}) // want "Found: TestClean/one" "Found: TestClean/two" "Found: TestClean/one#01"
	}
}

// A second control case, covering maps, fmt.Sprintf, concatenation and
// white-space escaping.
func TestCleanMap(t *testing.T) { // want "Found: TestCleanMap"
	tcs := map[string]testCase{
		"b": {name: "beta"},
		"a": {name: "alpha gamma"},
	}
	for k, tc := range tcs {
		t.Run(k+"/"+tc.name, func(t *testing.T) {}) // want "Found: TestCleanMap/a/alpha_gamma" "Found: TestCleanMap/b/beta"
	}
}
