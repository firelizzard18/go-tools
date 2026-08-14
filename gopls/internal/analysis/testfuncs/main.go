// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore

// The testfuncs command applies the testfuncs analyzer to the specified
// packages of Go source code.
//
// Usage:
//
//	testfuncs <mode> [flags] packages...
//
// where mode is "hybrid" (resolve variables with SSA and syntax) or "ast"
// (resolve variables with syntax alone, and do not build SSA at all).
package main

import (
	"fmt"
	"os"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/singlechecker"
	"golang.org/x/tools/gopls/internal/analysis/testfuncs"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	var a *analysis.Analyzer
	switch os.Args[1] {
	case "hybrid":
		a = testfuncs.Analyzer
	case "ast":
		a = testfuncs.ASTAnalyzer
	default:
		usage()
	}

	// Remove the mode so that singlechecker sees only its own flags and the
	// package patterns.
	os.Args = append(os.Args[:1], os.Args[2:]...)

	singlechecker.Main(a)
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s <hybrid|ast> [flags] packages...\n", os.Args[0])
	os.Exit(2)
}
