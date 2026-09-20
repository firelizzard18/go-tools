package a

import (
	"testing"
)

func TestTable(t *testing.T) { // want `{"Name":"TestTable"}`
	cases := []struct {
		Name string
		X    int
	}{
		{"foo", 1}, // want `{"Name":"TestTable/foo"}`
		{"bar", 2}, // want `{"Name":"TestTable/bar"}`
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Log(c.X)
		})
	}
}
