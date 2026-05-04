package structure

import "testing"

type fakeIssue map[string]string

func (f fakeIssue) Field(name string) string { return f[name] }

func TestSplitFieldValue(t *testing.T) {
	cases := map[string][]string{
		"":               nil,
		"a":              {"a"},
		"a, b ,  c":      {"a", "b", "c"},
		"single, ,empty": {"single", "empty"},
	}
	for in, want := range cases {
		got := splitFieldValue(in)
		if len(got) != len(want) {
			t.Fatalf("%q → %#v, want %#v", in, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%q → %#v, want %#v", in, got, want)
			}
		}
	}
}
