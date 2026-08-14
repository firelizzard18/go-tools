// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testfuncs

import (
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

// Packages a, b, and c must produce identical results in both modes; that is
// what makes the two comparable.
func TestAnalyzer(t *testing.T) {
	testdata := analysistest.TestData()

	t.Run("hybrid", func(t *testing.T) {
		analysistest.Run(t, testdata, Analyzer, "a", "b", "c")
	})

	t.Run("ast", func(t *testing.T) {
		analysistest.Run(t, testdata, ASTAnalyzer, "a", "b", "c")
	})
}

// TestHybridOnly covers the cases AST mode cannot resolve.
func TestHybridOnly(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "d")
}

// TestStats covers the -stats flag, which replaces the "Found:" diagnostics
// with one summary record per test function. Both modes must agree.
func TestStats(t *testing.T) {
	testdata := analysistest.TestData()

	for _, a := range []*analysis.Analyzer{Analyzer, ASTAnalyzer} {
		t.Run(a.Name, func(t *testing.T) {
			defer withFlag(t, a, "stats", "true")()
			analysistest.Run(t, testdata, a, "stats")
		})
	}
}

// withFlag sets a flag on the analyzer's flag set for the duration of a test,
// returning a function that restores it. The analyzers are package-level
// values shared by every test in this file.
func withFlag(t *testing.T, a *analysis.Analyzer, name, value string) func() {
	f := a.Flags.Lookup(name)
	if f == nil {
		t.Fatalf("%s has no -%s flag", a.Name, name)
	}
	prev := f.Value.String()
	err := f.Value.Set(value)
	if err != nil {
		t.Fatal(err)
	}
	return func() { f.Value.Set(prev) }
}
