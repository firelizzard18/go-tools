// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package a mirrors the coverage checklist of the hongxiang-demo analyzer.
// Every case it classifies as statically determinable should produce names
// here, and every case it rejects should produce none.
//
// Both analyzer modes must agree on this package.
package a

import (
	"fmt"
	"strconv"
	"testing"
)

// Helper function that is NOT a literal
func generateFromJson() string {
	return "test-json"
}

// Helper returning slice
func getTestCases() []struct{ Name string } {
	return []struct{ Name string }{{Name: "t1"}}
}

// 1. Field as Name (tc.XX) - not-manipulated
func TestFieldNotManipulated(t *testing.T) { // want "Found: TestFieldNotManipulated"
	tcs := []struct {
		name string
		val  int
	}{
		{name: "sub1", val: 1},
		{name: "sub2", val: 2},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {}) // want "Found: TestFieldNotManipulated/sub1" "Found: TestFieldNotManipulated/sub2"
	}
}

// 2. Field as Name (tc.XX) - manipulated (concatenation)
func TestFieldManipulatedAdd(t *testing.T) { // want "Found: TestFieldManipulatedAdd"
	tcs := []struct {
		name string
	}{
		{name: "sub1"},
	}
	for _, tc := range tcs {
		t.Run(tc.name+"/suffix", func(t *testing.T) {}) // want "Found: TestFieldManipulatedAdd/sub1/suffix"
	}
}

// 3. Field as Name (tc.XX) - manipulated (fmt.Sprintf)
func TestFieldManipulatedSprintf(t *testing.T) { // want "Found: TestFieldManipulatedSprintf"
	tcs := []struct {
		name string
	}{
		{name: "sub1"},
	}
	for _, tc := range tcs {
		t.Run(fmt.Sprintf("%s/suffix", tc.name), func(t *testing.T) {}) // want "Found: TestFieldManipulatedSprintf/sub1/suffix"
	}
}

// 4. Key as Name - not-manipulated
func TestKeyNotManipulated(t *testing.T) { // want "Found: TestKeyNotManipulated"
	tcs := map[string]struct{}{
		"sub1": {},
		"sub2": {},
	}
	for name := range tcs {
		t.Run(name, func(t *testing.T) {}) // want "Found: TestKeyNotManipulated/sub1" "Found: TestKeyNotManipulated/sub2"
	}
}

// 5. Index as Name - not-manipulated
func TestIndexNotManipulated(t *testing.T) { // want "Found: TestIndexNotManipulated"
	tcs := []struct{ val int }{
		{val: 1},
	}
	for i := range tcs {
		t.Run(strconv.Itoa(i), func(t *testing.T) {}) // want "Found: TestIndexNotManipulated/0"
	}
}

// 6. Index as Name - manipulated
func TestIndexManipulated(t *testing.T) { // want "Found: TestIndexManipulated"
	tcs := []struct{ val int }{
		{val: 1},
	}
	for i := range tcs {
		t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {}) // want "Found: TestIndexManipulated/test-0"
	}
}

// 7. Not statically determined (slice returned from function)
func TestNotStaticallyDeterminedFunc(t *testing.T) { // want "Found: TestNotStaticallyDeterminedFunc"
	tcs := getTestCases()
	for _, tc := range tcs {
		t.Run(tc.Name, func(t *testing.T) {})
	}
}

// 8. Not statically determined (field value returned from function)
func TestNotStaticallyDeterminedFieldFunc(t *testing.T) { // want "Found: TestNotStaticallyDeterminedFieldFunc"
	tcs := []struct{ name string }{
		{name: generateFromJson()},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {})
	}
}

// 9. Not statically determined (slice modified / referenced multiple times)
func TestNotStaticallyDeterminedModified(t *testing.T) { // want "Found: TestNotStaticallyDeterminedModified"
	tcs := []struct{ name string }{
		{name: "sub1"},
	}
	tcs[0].name = "modified" // third reference to tcs
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {})
	}
}

// 10. Function-manipulated (wrapped in custom function)
func TestFunctionManipulated(t *testing.T) { // want "Found: TestFunctionManipulated"
	tcs := []struct{ name string }{
		{name: "sub1"},
	}
	customFormat := func(s string) string { return s + "-ok" }
	for _, tc := range tcs {
		t.Run(customFormat(tc.name), func(t *testing.T) {})
	}
}

// 11. Field name is not "name" (tc.scenario)
func TestFieldNotName(t *testing.T) { // want "Found: TestFieldNotName"
	tcs := []struct {
		scenario string
	}{
		{scenario: "sub1"},
	}
	for _, tc := range tcs {
		t.Run(tc.scenario, func(t *testing.T) {}) // want "Found: TestFieldNotName/sub1"
	}
}

// 12. Not statically determined (map key is function call)
func TestMapKeyFromFunc(t *testing.T) { // want "Found: TestMapKeyFromFunc"
	tcs := map[string]struct{}{
		generateFromJson(): {},
	}
	for name := range tcs {
		t.Run(name, func(t *testing.T) {})
	}
}

// 13. Not statically determined (map key is method call)
type helper struct{}

func (helper) getName() string { return "method" }

func TestMapKeyFromMethod(t *testing.T) { // want "Found: TestMapKeyFromMethod"
	h := helper{}
	tcs := map[string]struct{}{
		h.getName(): {},
	}
	for name := range tcs {
		t.Run(name, func(t *testing.T) {})
	}
}

// 14. Case 2 - const name (no loop)
func TestConstNoLoop(t *testing.T) { // want "Found: TestConstNoLoop"
	t.Run("one", func(t *testing.T) {}) // want "Found: TestConstNoLoop/one"
}

// 15. Case 2 - const name (for loop, i.e. TestRace pattern)
//
// The three-clause for statement is not interpreted, so the number of subtests
// (and hence their "#NN" suffixes) is unknown. Reporting "SetColorProfile"
// alone would be wrong.
func TestConstForLoop(t *testing.T) { // want "Found: TestConstForLoop"
	for i := 0; i < 10; i++ {
		t.Run("SetColorProfile", func(t *testing.T) {})
	}
}

// 16. Case 2 - constant identifier
func TestConstIdent(t *testing.T) { // want "Found: TestConstIdent"
	const myTestName = "my-test"
	t.Run(myTestName, func(t *testing.T) {}) // want "Found: TestConstIdent/my-test"
}

func TestNonConstIdent(t *testing.T) { // want "Found: TestNonConstIdent"
	t.Run(fmt.Sprintf("%s-%d", "foo", 1), func(t *testing.T) {}) // want "Found: TestNonConstIdent/foo-1"
}

// 17. Multiple name sources (field and index) - manipulated
func TestMultipleSources(t *testing.T) { // want "Found: TestMultipleSources"
	tcs := []struct {
		name string
	}{
		{name: "sub1"},
	}
	for i, tc := range tcs {
		t.Run(fmt.Sprintf("%s-%d", tc.name, i), func(t *testing.T) {}) // want "Found: TestMultipleSources/sub1-0"
	}
}

// 18. Multiple name sources (multiple fields) - manipulated
func TestMultipleFields(t *testing.T) { // want "Found: TestMultipleFields"
	tcs := []struct {
		name string
		mode string
	}{
		{name: "sub1", mode: "HTTP"},
	}
	for _, tc := range tcs {
		t.Run(tc.name+"-"+tc.mode, func(t *testing.T) {}) // want "Found: TestMultipleFields/sub1-HTTP"
	}
}

// 19. Unrecognized name expression
func TestUnrecognizedName(t *testing.T) { // want "Found: TestUnrecognizedName"
	tcs := []struct{ name string }{{name: "sub1"}}
	for range tcs {
		x := generateFromJson()
		t.Run(x, func(t *testing.T) {})
	}
}

// 20. Map multiple name sources (key and field) - manipulated
func TestMapMultipleSources(t *testing.T) { // want "Found: TestMapMultipleSources"
	tcs := map[string]struct{ mode string }{
		"sub1": {mode: "HTTP"},
	}
	for name, tc := range tcs {
		t.Run(name+"-"+tc.mode, func(t *testing.T) {}) // want "Found: TestMapMultipleSources/sub1-HTTP"
	}
}

func ExampleFoo() { // want "Found: ExampleFoo"
	// Output: Hi
	fmt.Println("Hi")
}

func BenchmarkFoo(b *testing.B) { // want "Found: BenchmarkFoo"
	b.Log("Hi")
}

func FuzzFoo(f *testing.F) { // want "Found: FuzzFoo"
	f.Add(1)

	f.Fuzz(func(t *testing.T, x int) {
		t.Log(x)
	})
}
