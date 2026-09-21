package with_errors

import (
	"testing"
)

// Tests in a package that does not typecheck are still reported, but the
// analyzer must not report anything it cannot verify. And it must not panic.

func TestValid(t *testing.T) { // want `{"Name":"TestValid"}`
	t.Run("sub", func(t *testing.T) {}) // want `{"Name":"TestValid/sub"}`
}

func TestUndefinedCall(t *testing.T) { // want `{"Name":"TestUndefinedCall"}`
	undefinedFunc()
	t.Run("sub", func(t *testing.T) {}) // want `{"Name":"TestUndefinedCall/sub"}`
}

func TestUndefinedCallWithTB(t *testing.T) { // want `{"Name":"TestUndefinedCallWithTB"}`
	undefinedFunc(t) // want `{"Name":"TestUndefinedCallWithTB","Error":"escapes"`
	t.Run("sub", func(t *testing.T) {})
}

func TestTooManyArgs(t *testing.T) { // want `{"Name":"TestTooManyArgs"}`
	oneParam("x", t) // want `{"Name":"TestTooManyArgs","Error":"escapes"`
	t.Run("sub", func(t *testing.T) {})
}

func oneParam(t *testing.T) {}

func TestUndefinedName(t *testing.T) { // want `{"Name":"TestUndefinedName"}`
	t.Run(undefinedName, func(t *testing.T) {}) // want `{"Name":"TestUndefinedName","Error":"unresolved"`
}

func TestUndefinedCallback(t *testing.T) { // want `{"Name":"TestUndefinedCallback"}`
	t.Run("sub", undefinedCallback) // want `{"Name":"TestUndefinedCallback/sub"}` `{"Name":"TestUndefinedCallback/sub","Error":"unresolved"`
}

func TestWrongCallback(t *testing.T) { // want `{"Name":"TestWrongCallback"}`
	t.Run("sub", func(s string) {}) // want `{"Name":"TestWrongCallback/sub"}` `{"Name":"TestWrongCallback/sub","Error":"unresolved"`
}

func TestUndefinedParam(t *undefinedType) {
	t.Run("sub", func(t *undefinedType) {})
}

func TestNotCallable(t *testing.T) { // want `{"Name":"TestNotCallable"}`
	run := 3
	run("x", func(t *testing.T) {})
	t.Run("sub", func(t *testing.T) {}) // want `{"Name":"TestNotCallable/sub"}`
}

func TestRedeclared(t *testing.T) { // want `{"Name":"TestRedeclared"}`
	t.Run("first", func(t *testing.T) {}) // want `{"Name":"TestRedeclared/first"}`
}

func TestRedeclared(t *testing.T) {
	t.Run("second", func(t *testing.T) {})
}

func notAVarFunc() {}

func TestRangeKeyIsFunc(t *testing.T) { // want `{"Name":"TestRangeKeyIsFunc"}`
	for notAVarFunc = range []string{"a", "b"} { // want `{"Name":"TestRangeKeyIsFunc/sub"}` `{"Name":"TestRangeKeyIsFunc/sub#01"}`
		t.Run("sub", func(t *testing.T) {})
	}
}

type notAVarType int

func TestRangeKeyIsType(t *testing.T) { // want `{"Name":"TestRangeKeyIsType"}`
	for notAVarType = range []string{"a", "b"} { // want `{"Name":"TestRangeKeyIsType/sub"}` `{"Name":"TestRangeKeyIsType/sub#01"}`
		t.Run("sub", func(t *testing.T) {})
	}
}

func TestRangeKeyIsPackage(t *testing.T) { // want `{"Name":"TestRangeKeyIsPackage"}`
	for strings = range []string{"a", "b"} { // want `{"Name":"TestRangeKeyIsPackage/sub"}` `{"Name":"TestRangeKeyIsPackage/sub#01"}`
		t.Run("sub", func(t *testing.T) {})
	}
}

func TestRangeKeyUndefined(t *testing.T) { // want `{"Name":"TestRangeKeyUndefined"}`
	for undefinedKey = range []string{"a", "b"} { // want `{"Name":"TestRangeKeyUndefined/sub"}` `{"Name":"TestRangeKeyUndefined/sub#01"}`
		t.Run("sub", func(t *testing.T) {})
	}
}
