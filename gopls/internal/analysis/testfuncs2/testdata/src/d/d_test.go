package d

import "testing"

// Adversarial cases for analyzeRange. Every `want` here is written against the
// rule, not against the code: each case must report the correct name or nothing
// at all. Cases that currently panic or report a name the test never runs are
// the point of this package.

type testCase struct {
	Name string
	Skip bool
}

// Control: the case that is supposed to work.
func TestTable(t *testing.T) { // want `{"Name":"TestTable"}`
	cases := []testCase{{Name: "foo"}, {Name: "bar"}}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {}) // want `{"Name":"TestTable/foo"}` `{"Name":"TestTable/bar"}`
	}
}

// Panic: the range operand does not resolve (package-level var), so
// evaluateAs[seqValue] returns a nil seqValue. analyzeRange records the error
// and falls through instead of returning, then calls All() on it.
func TestUnresolvableTable(t *testing.T) { // want `{"Name":"TestUnresolvableTable","Errors":\[`
	for _, c := range pkgCases {
		t.Run(c.Name, func(t *testing.T) {})
	}
}

var pkgCases = []testCase{{Name: "foo"}}

// Panic: go/types creates no var object for the blank in a non-DEFINE range, so
// ObjectOf returns nil and the unchecked type assertion on stmt.Key fails.
func TestBlankInAssignRange(t *testing.T) { // want `{"Name":"TestBlankInAssignRange"}`
	cases := []testCase{{Name: "foo"}}
	var c testCase
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {}) // want `{"Name":"TestBlankInAssignRange/foo"}`
	}
}

// Wrong name: the loop variable is bound in env, which bypasses the mention
// counting that resolveVar uses to prove immutability. The subtest is named
// "bar", but we resolve c.Name out of the table entry and report "foo".
func TestLoopVarFieldWrite(t *testing.T) { // want `{"Name":"TestLoopVarFieldWrite","Errors":\[`
	cases := []testCase{{Name: "foo"}}
	for _, c := range cases {
		c.Name = "bar"
		t.Run(c.Name, func(t *testing.T) {})
	}
}

// Wrong name: same hole, reached by replacing the loop variable outright.
func TestLoopVarReassigned(t *testing.T) { // want `{"Name":"TestLoopVarReassigned","Errors":\[`
	cases := []testCase{{Name: "foo"}}
	other := testCase{Name: "bar"}
	for _, c := range cases {
		c = other
		t.Run(c.Name, func(t *testing.T) {})
	}
}

// Wrong name: the loop variable's address escapes to a helper that mutates it.
func TestLoopVarAddressEscapes(t *testing.T) { // want `{"Name":"TestLoopVarAddressEscapes","Errors":\[`
	cases := []testCase{{Name: "foo"}}
	for _, c := range cases {
		rename(&c)
		t.Run(c.Name, func(t *testing.T) {})
	}
}

func rename(c *testCase) { c.Name = "bar" }

// Wrong name: compound assignment to a field of the loop variable.
func TestLoopVarFieldAppend(t *testing.T) { // want `{"Name":"TestLoopVarFieldAppend","Errors":\[`
	cases := []testCase{{Name: "foo"}}
	for _, c := range cases {
		c.Name += "x"
		t.Run(c.Name, func(t *testing.T) {})
	}
}

// Wrong name: the key is what gets mutated.
func TestLoopKeyWrite(t *testing.T) { // want `{"Name":"TestLoopKeyWrite","Errors":\[`
	cases := []testCase{{Name: "foo"}}
	for i, c := range cases {
		i++
		t.Run(c.Name, func(t *testing.T) {})
		_ = i
	}
}

// Latent. A continue guard means the subtest is never run, but today `analyze`
// has no IfStmt arm so the whole test taints, which is the safe direction. This
// case exists to fail the moment halting replaces tainting without the
// abort/divergence distinction landing alongside it.
func TestContinueGuard(t *testing.T) { // want `{"Name":"TestContinueGuard","Errors":\[`
	cases := []testCase{{Name: "foo", Skip: true}}
	for _, c := range cases {
		if c.Skip {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {})
	}
}

func TestUnmodeledAfterRun(t *testing.T) { // want `{"Name":"TestUnmodeledAfterRun"}`
	cases := []testCase{{Name: "foo"}, {Name: "bar"}}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {}) // want `{"Name":"TestUnmodeledAfterRun/foo"}` `{"Name":"TestUnmodeledAfterRun/bar#01"}`
		runMore(t)
	}
}

func runMore(t *testing.T) {
	t.Run("bar", func(t *testing.T) {}) // want `{"Name":"TestUnmodeledAfterRun/bar"}` `{"Name":"TestUnmodeledAfterRun/bar#02"}`
}

// A sibling after an unresolved loop. If the loop is silently skipped rather
// than tainting, this reports "dup" when the true name depends on how many
// times the loop ran.
func TestShiftedSibling(t *testing.T) { // want `{"Name":"TestShiftedSibling","Errors":\[`
	for _, c := range pkgCases {
		t.Run(c.Name, func(t *testing.T) {})
	}
	t.Run("foo", func(t *testing.T) {})
}
