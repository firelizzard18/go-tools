package b

import "testing"

type TC struct {
	Name string
	Fn   func(*testing.T)
}

func Test(t *testing.T) {
	tc := []TC{
		{Name: "foo", Fn: test},
		{Name: "baz", Fn: test},
	}
	for _, tc := range tc {
		t.Run(tc.Name, tc.Fn)
	}
}

func test(t *testing.T) {
	for range 2 {
		t.Run("bar", func(t *testing.T) {})
	}
}
