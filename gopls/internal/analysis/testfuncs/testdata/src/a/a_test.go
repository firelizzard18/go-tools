package a

import (
	"fmt"
	"strconv"
	"testing"
)

// Helper function that is NOT a literal
func generateFromJson() string {
	return "test-json"
}

// Helper returning slice
func getTestCases() []struct{ Name string } {
	return []struct{ Name string }{{Name: "t1"}}
}

// 1. Field as Name (tc.XX) - not-manipulated
func TestFieldNotManipulated(t *testing.T) { // want "TestFieldNotManipulated: 1 t.Run call \\(field as name, not-manipulated\\)"
	tcs := []struct {
		name string
		val  int
	}{
		{name: "sub1", val: 1},
		{name: "sub2", val: 2},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {})
	}
}

// 2. Field as Name (tc.XX) - manipulated (concatenation)
func TestFieldManipulatedAdd(t *testing.T) { // want "TestFieldManipulatedAdd: 1 t.Run call \\(field as name, manipulated\\)"
	tcs := []struct {
		name string
	}{
		{name: "sub1"},
	}
	for _, tc := range tcs {
		t.Run(tc.name+"/suffix", func(t *testing.T) {})
	}
}

// 3. Field as Name (tc.XX) - manipulated (fmt.Sprintf)
func TestFieldManipulatedSprintf(t *testing.T) { // want "TestFieldManipulatedSprintf: 1 t.Run call \\(field as name, manipulated\\)"
	tcs := []struct {
		name string
	}{
		{name: "sub1"},
	}
	for _, tc := range tcs {
		t.Run(fmt.Sprintf("%s/suffix", tc.name), func(t *testing.T) {})
	}
}

// 4. Key as Name - not-manipulated
func TestKeyNotManipulated(t *testing.T) { // want "TestKeyNotManipulated: 1 t.Run call \\(key as name, not-manipulated\\)"
	tcs := map[string]struct{}{
		"sub1": {},
		"sub2": {},
	}
	for name := range tcs {
		t.Run(name, func(t *testing.T) {})
	}
}

// 5. Index as Name - not-manipulated
func TestIndexNotManipulated(t *testing.T) { // want "TestIndexNotManipulated: 1 t.Run call \\(index as name, not-manipulated\\)"
	tcs := []struct{ val int }{
		{val: 1},
	}
	for i := range tcs {
		t.Run(strconv.Itoa(i), func(t *testing.T) {})
	}
}

// 6. Index as Name - manipulated
func TestIndexManipulated(t *testing.T) { // want "TestIndexManipulated: 1 t.Run call \\(index as name, manipulated\\)"
	tcs := []struct{ val int }{
		{val: 1},
	}
	for i := range tcs {
		t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {})
	}
}

// 7. Not statically determined (slice returned from function)
func TestNotStaticallyDeterminedFunc(t *testing.T) { // want "TestNotStaticallyDeterminedFunc: 1 t.Run call \\(not statically determined\\)"
	tcs := getTestCases()
	for _, tc := range tcs {
		t.Run(tc.Name, func(t *testing.T) {})
	}
}

// 8. Not statically determined (field value returned from function)
func TestNotStaticallyDeterminedFieldFunc(t *testing.T) { // want "TestNotStaticallyDeterminedFieldFunc: 1 t.Run call \\(not statically determined\\)"
	tcs := []struct{ name string }{
		{name: generateFromJson()},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {})
	}
}

// 9. Not statically determined (slice modified / referenced multiple times)
func TestNotStaticallyDeterminedModified(t *testing.T) { // want "TestNotStaticallyDeterminedModified: 1 t.Run call \\(not statically determined\\)"
	tcs := []struct{ name string }{
		{name: "sub1"},
	}
	tcs[0].name = "modified" // third reference to tcs
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {})
	}
}

// 10. Function-manipulated (wrapped in custom function)
func TestFunctionManipulated(t *testing.T) { // want "TestFunctionManipulated: 1 t.Run call \\(unsupport function\\)"
	tcs := []struct{ name string }{
		{name: "sub1"},
	}
	customFormat := func(s string) string { return s + "-ok" }
	for _, tc := range tcs {
		t.Run(customFormat(tc.name), func(t *testing.T) {})
	}
}

// 11. Field name is not "name" (tc.scenario)
func TestFieldNotName(t *testing.T) { // want "TestFieldNotName: 1 t.Run call \\(field as name, not-manipulated\\)"
	tcs := []struct {
		scenario string
	}{
		{scenario: "sub1"},
	}
	for _, tc := range tcs {
		t.Run(tc.scenario, func(t *testing.T) {})
	}
}

// 12. Not statically determined (map key is function call)
func TestMapKeyFromFunc(t *testing.T) { // want "TestMapKeyFromFunc: 1 t.Run call \\(not statically determined\\)"
	tcs := map[string]struct{}{
		generateFromJson(): {},
	}
	for name := range tcs {
		t.Run(name, func(t *testing.T) {})
	}
}

// 13. Not statically determined (map key is method call)
type helper struct{}

func (helper) getName() string { return "method" }

func TestMapKeyFromMethod(t *testing.T) { // want "TestMapKeyFromMethod: 1 t.Run call \\(not statically determined\\)"
	h := helper{}
	tcs := map[string]struct{}{
		h.getName(): {},
	}
	for name := range tcs {
		t.Run(name, func(t *testing.T) {})
	}
}

// 14. Case 2 - const name (no loop)
func TestConstNoLoop(t *testing.T) { // want "TestConstNoLoop: 1 t.Run call \\(const\\)"
	t.Run("one", func(t *testing.T) {})
}

// 15. Case 2 - const name (for loop, i.e. TestRace pattern)
func TestConstForLoop(t *testing.T) { // want "TestConstForLoop: 1 t.Run call \\(const\\)"
	for i := 0; i < 10; i++ {
		t.Run("SetColorProfile", func(t *testing.T) {})
	}
}

// 16. Case 2 - constant identifier
func TestConstIdent(t *testing.T) { // want "TestConstIdent: 1 t.Run call \\(const\\)"
	const myTestName = "my-test"
	t.Run(myTestName, func(t *testing.T) {})
}

// 17. Multiple name sources (field and index) - manipulated
func TestMultipleSources(t *testing.T) { // want "TestMultipleSources: 1 t.Run call \\(index and field as name, manipulated\\)"
	tcs := []struct {
		name string
	}{
		{name: "sub1"},
	}
	for i, tc := range tcs {
		t.Run(fmt.Sprintf("%s-%d", tc.name, i), func(t *testing.T) {})
	}
}

// 18. Multiple name sources (multiple fields) - manipulated
func TestMultipleFields(t *testing.T) { // want "TestMultipleFields: 1 t.Run call \\(field as name, manipulated\\)"
	tcs := []struct {
		name string
		mode string
	}{
		{name: "sub1", mode: "HTTP"},
	}
	for _, tc := range tcs {
		t.Run(tc.name+"-"+tc.mode, func(t *testing.T) {})
	}
}

// 19. Unrecognized name expression
func TestUnrecognizedName(t *testing.T) { // want "TestUnrecognizedName: 1 t.Run call \\(not recoganized\\)"
	tcs := []struct{ name string }{{name: "sub1"}}
	for range tcs {
		var x string
		t.Run(x, func(t *testing.T) {})
	}
}

// 20. Map multiple name sources (key and field) - manipulated
func TestMapMultipleSources(t *testing.T) { // want "TestMapMultipleSources: 1 t.Run call \\(key and field as name, manipulated\\)"
	tcs := map[string]struct{ mode string }{
		"sub1": {mode: "HTTP"},
	}
	for name, tc := range tcs {
		t.Run(name+"-"+tc.mode, func(t *testing.T) {})
	}
}

func ExampleFoo() {
	// Output: Hi
	fmt.Println("Hi")
}

func BenchmarkFoo(b *testing.B) {
	b.Log("Hi")
}

func FuzzFoo(f *testing.F) {
	f.Add(1)

	f.Fuzz(func(t *testing.T, x int) {
		t.Log(x)
	})
}
