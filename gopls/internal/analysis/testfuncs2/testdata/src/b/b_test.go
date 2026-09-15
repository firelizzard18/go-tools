package a

import (
	"testing"
)

func TestTable(t *testing.T) { // want `{"Name":"TestTable"}`
	cases := []struct{ Name string }{
		{"foo"}, // want `{"Name":"TestTable/foo"}`
		{"bar"}, // want `{"Name":"TestTable/bar"}`
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {})
	}
}
