package a

import (
	"testing"
)

func TestTable(t *testing.T) { // want `{"Name":"TestTable"}`
	cases := []testCase{
		{Name: "foo", X: 1}, // want `{"Name":"TestTable/foo"}`
		{Name: "bar", X: 2}, // want `{"Name":"TestTable/bar"}`
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Log(c.X)
		})
	}
}

func TestUnresolvableTable(t *testing.T) { // want `{"Name":"TestUnresolvableTable","Errors":\[`
	for _, c := range pkgCases {
		t.Run(c.Name, func(t *testing.T) {})
	}
}

func TestLoopVarFieldWrite(t *testing.T) { // want `{"Name":"TestLoopVarFieldWrite","Errors":\[`
	cases := []testCase{{Name: "foo"}}
	for _, c := range cases {
		c.Name = "bar"
		t.Run(c.Name, func(t *testing.T) {})
	}
}

func TestLoopVarReassigned(t *testing.T) { // want `{"Name":"TestLoopVarReassigned","Errors":\[`
	cases := []testCase{{Name: "foo"}}
	other := testCase{Name: "bar"}
	for _, c := range cases {
		c = other
		t.Run(c.Name, func(t *testing.T) {})
	}
}

func TestLoopVarAddressEscapes(t *testing.T) { // want `{"Name":"TestLoopVarAddressEscapes","Errors":\[`
	cases := []testCase{{Name: "foo"}}
	for _, c := range cases {
		rename(&c)
		t.Run(c.Name, func(t *testing.T) {})
	}
}

func TestLoopVarFieldAppend(t *testing.T) { // want `{"Name":"TestLoopVarFieldAppend","Errors":\[`
	cases := []testCase{{Name: "foo"}}
	for _, c := range cases {
		c.Name += "x"
		t.Run(c.Name, func(t *testing.T) {})
	}
}

// resolveVar's mention scan stops at the read, so the write is never seen. In a
// loop body that ordering is backwards: iteration 2's read sees iteration 1's
// write.
func TestLoopVarWriteAfterRead(t *testing.T) { // want `{"Name":"TestLoopVarWriteAfterRead","Errors":\["unresolved"`
	cases := []testCase{{Name: "foo"}, {Name: "bar"}}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {})
		c.Name = "zzz"
	}
}

func TestUnsupportedStmt(t *testing.T) { // want `{"Name":"TestUnsupportedStmt","Errors":\[`
	cases := []testCase{{Name: "foo", Skip: true}}
	for _, c := range cases {
		if c.Skip {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {})
	}
}

func TestRunAfterRun(t *testing.T) { // want `{"Name":"TestRunAfterRun"}`
	cases := []testCase{
		{Name: "foo"}, // want `{"Name":"TestRunAfterRun/foo"}`
		{Name: "bar"}, // want `{"Name":"TestRunAfterRun/bar#01"}`
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {})
		testRunAfterRun(t)
	}
}

func testRunAfterRun(t *testing.T) {
	t.Run("bar", func(t *testing.T) {}) // want `{"Name":"TestRunAfterRun/bar"}` `{"Name":"TestRunAfterRun/bar#02"}`
}

func TestRunAfterRange(t *testing.T) { // want `{"Name":"TestRunAfterRange"}`
	cases := []testCase{
		{Name: "foo", X: 1}, // want `{"Name":"TestRunAfterRange/foo"}`
		{Name: "bar", X: 2}, // want `{"Name":"TestRunAfterRange/bar"}`
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {})
	}
	t.Run("foo", func(t *testing.T) {}) // want `{"Name":"TestRunAfterRange/foo#01"}`
}

func TestRunAfterBadRange(t *testing.T) { // want `{"Name":"TestRunAfterBadRange","Errors":\[`
	for _, c := range pkgCases {
		t.Run(c.Name, func(t *testing.T) {})
	}
	t.Run("foo", func(t *testing.T) {})
}

func rename(c *testCase) { c.Name = "bar" }

var pkgCases = []testCase{{Name: "foo"}}

type testCase struct {
	Name string
	X    int
	Skip bool
}
