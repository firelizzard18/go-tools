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

func TestUndefinedCallWithTB(t *testing.T) { // want `{"Name":"TestUndefinedCallWithTB","Tainted":"TB escapes the test"}`
	undefinedFunc(t)
	t.Run("sub", func(t *testing.T) {})
}

func TestUndefinedName(t *testing.T) { // want `{"Name":"TestUndefinedName","Tainted":"cannot determine subtest name`
	t.Run(undefinedName, func(t *testing.T) {})
}

func TestUndefinedCallback(t *testing.T) { // want `{"Name":"TestUndefinedCallback","Tainted":"invalid callback \(not a function\?\)"}`
	t.Run("sub", undefinedCallback)
}

func TestWrongCallback(t *testing.T) { // want `{"Name":"TestWrongCallback","Tainted":"invalid callback: wrong signature"}`
	t.Run("sub", func(s string) {})
}

func TestUndefinedParam(t *undefinedType) {
	t.Run("sub", func(t *undefinedType) {})
}
