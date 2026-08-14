// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testfuncs

import (
	"testing"

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
