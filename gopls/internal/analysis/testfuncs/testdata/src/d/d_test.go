// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package d holds the cases where hybrid mode is strictly stronger than AST
// mode. It is only run against the hybrid analyzer.
package d

import "testing"

// A variable declared with no initializer has no binding site for AST mode to
// find, so AST mode reports nothing. SSA knows it holds the zero value, and
// the testing package names an empty subtest "#00".
func TestZeroValue(t *testing.T) { // want "Found: TestZeroValue"
	tcs := []struct{ name string }{{name: "sub1"}}
	for range tcs {
		var x string
		t.Run(x, func(t *testing.T) {}) // want "Found: TestZeroValue/#00"
	}
}
