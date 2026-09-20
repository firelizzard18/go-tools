package with_errors

import "testing"

// Tests in a package that does not typecheck are still reported, but the
// analyzer must not report anything it cannot verify.

func TestValid(t *testing.T) { // want `{"Name":"TestValid"}`
	t.Run("sub", func(t *testing.T) {}) // want `{"Name":"TestValid/sub"}`
}

func TestUndefinedCall(t *testing.T) { // want `{"Name":"TestUndefinedCall"}`
	undefinedFunc()
	t.Run("sub", func(t *testing.T) {}) // want `{"Name":"TestUndefinedCall/sub"}`
}

func TestUndefinedCallWithTB(t *testing.T) { // want `{"Name":"TestUndefinedCallWithTB","Errors":\[{"Kind":"escapes","Message":"TB passed to call: cannot determine function signature"}\]}`
	undefinedFunc(t)
	t.Run("sub", func(t *testing.T) {})
}

// Arity does not match, so the TB is at an argument index past the end of the
// callee's parameter list.
func TestTooManyArgs(t *testing.T) { // want `{"Name":"TestTooManyArgs","Errors":\[{"Kind":"escapes","Message":"TB passed to call as invalid argument"}\]}`
	oneParam("x", t)
	t.Run("sub", func(t *testing.T) {})
}

func oneParam(t *testing.T) {}

func TestUndefinedName(t *testing.T) { // want `{"Name":"TestUndefinedName","Errors":\[{"Kind":"unknown","Message":"cannot determine subtest name: cannot resolve undefinedName"}\]}`
	t.Run(undefinedName, func(t *testing.T) {})
}

func TestUndefinedCallback(t *testing.T) { // want `{"Name":"TestUndefinedCallback"}`
	t.Run("sub", undefinedCallback) // want `{"Name":"TestUndefinedCallback/sub","Errors":\[{"Kind":"unknown","Message":"cannot determine callback: cannot resolve undefinedCallback"}\]}`
}

func TestWrongCallback(t *testing.T) { // want `{"Name":"TestWrongCallback"}`
	t.Run("sub", func(s string) {}) // want `{"Name":"TestWrongCallback/sub","Errors":\[{"Kind":"unknown","Message":"cannot determine callback: cannot resolve function literal"}\]}`
}

func TestUndefinedParam(t *undefinedType) {
	t.Run("sub", func(t *undefinedType) {})
}
