// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package testfuncs defines an Analyzer that locates test functions and
// statically analyzes the subtests they produce.
//
// # Analyzer testfuncs
//
// testfuncs: extract tests and subtests through static analysis
package testfuncs
