// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package b covers recursion into a subtest callback that is itself named by a
// struct field, and the "#NN" disambiguation of repeated names.
package b

import "testing"

type TC struct {
	Name string
	Fn   func(*testing.T)
}

func Test(t *testing.T) { // want "Found: Test$"
	tc := []TC{
		{Name: "foo", Fn: test},
		{Name: "baz", Fn: test},
	}
	for _, tc := range tc {
		t.Run(tc.Name, tc.Fn) // want "Found: Test/foo$" "Found: Test/baz$"
	}
}

func test(t *testing.T) {
	for range 2 {
		t.Run("bar", func(t *testing.T) {}) // want "Found: Test/foo/bar$" "Found: Test/foo/bar#01$" "Found: Test/baz/bar$" "Found: Test/baz/bar#01$"
	}
}
