package with_errors

import "testing"

// Tests in a package that does not typecheck are still reported, but the
// analyzer must not report anything it cannot verify.

func TestValid(t *testing.T) { // want "Found: TestValid"
	t.Run("sub", func(t *testing.T) {}) // want "Found: TestValid/sub"
}

// An undefined call does not prevent subtest discovery.
func TestUndefinedCall(t *testing.T) { // want "Found: TestUndefinedCall"
	undefinedFunc()
	t.Run("sub", func(t *testing.T) {}) // want "Found: TestUndefinedCall/sub"
}

// Passing the TB to an undefined call taints it.
func TestUndefinedCallWithTB(t *testing.T) { // want "Found: TestUndefinedCallWithTB" "Tainted \\(can't report children\\): TestUndefinedCallWithTB"
	undefinedFunc(t)
	t.Run("sub", func(t *testing.T) {})
}

// An undefined name expression is not statically known.
func TestUndefinedName(t *testing.T) { // want "Found: TestUndefinedName" "Tainted \\(can't report children\\): TestUndefinedName"
	t.Run(undefinedName, func(t *testing.T) {})
}

// An undefined callback has no signature.
func TestUndefinedCallback(t *testing.T) { // want "Found: TestUndefinedCallback" "Tainted \\(can't report children\\): TestUndefinedCallback"
	t.Run("sub", undefinedCallback)
}

// A callback with the wrong signature.
func TestWrongCallback(t *testing.T) { // want "Found: TestWrongCallback" "Tainted \\(can't report children\\): TestWrongCallback"
	t.Run("sub", func(s string) {})
}

// The parameter type is undefined, so this is not recognized as a test.
func TestUndefinedParam(t *undefinedType) {
	t.Run("sub", func(t *undefinedType) {})
}
