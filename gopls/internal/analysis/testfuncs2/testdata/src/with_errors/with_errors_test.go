package with_errors

import "testing"

// Tests in a package that does not typecheck are still reported, but the
// analyzer must not report anything it cannot verify.

func TestValid(t *testing.T) { // want `{"Name":"TestValid"}`
	t.Run("sub", func(t *testing.T) {}) // want `{"Name":"TestValid/sub"}`
}

// An undefined call does not prevent subtest discovery.
func TestUndefinedCall(t *testing.T) { // want `{"Name":"TestUndefinedCall"}`
	undefinedFunc()
	t.Run("sub", func(t *testing.T) {}) // want `{"Name":"TestUndefinedCall/sub"}`
}

// Passing the TB to an undefined call taints it.
func TestUndefinedCallWithTB(t *testing.T) { // want `{"Name":"TestUndefinedCallWithTB","Tainted":"non-call reference"}`
	undefinedFunc(t)
	t.Run("sub", func(t *testing.T) {})
}

// An undefined name expression is not statically known.
func TestUndefinedName(t *testing.T) { // want `{"Name":"TestUndefinedName","Tainted":"cannot determine subtest name"}`
	t.Run(undefinedName, func(t *testing.T) {})
}

// An undefined callback has no signature.
func TestUndefinedCallback(t *testing.T) { // want `{"Name":"TestUndefinedCallback","Tainted":"invalid callback \(not a function\?\)"}`
	t.Run("sub", undefinedCallback)
}

// A callback with the wrong signature.
func TestWrongCallback(t *testing.T) { // want `{"Name":"TestWrongCallback","Tainted":"invalid callback: wrong signature"}`
	t.Run("sub", func(s string) {})
}

// The parameter type is undefined, so this is not recognized as a test.
func TestUndefinedParam(t *undefinedType) {
	t.Run("sub", func(t *undefinedType) {})
}
