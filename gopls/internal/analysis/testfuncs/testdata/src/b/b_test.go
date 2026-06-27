package b

import "testing"

type TC struct {
	Name string
	Fn   func(*testing.T)
}

func Test(t *testing.T) {
	tc := TC{Name: "foo", Fn: test}
	t.Run(tc.Name, tc.Fn)
}

func test(t *testing.T) {
	t.Run("bar", func(t *testing.T) {})
}
