package structure

import "testing"

func TestBuiltins_DefaultEvaluatesCleanly(t *testing.T) {
	def := Default("BIL")
	if err := Validate(&def); err != nil {
		t.Fatalf("default invalid: %v", err)
	}
	if def.ID != BuiltinDefaultID {
		t.Fatalf("id = %q, want %q", def.ID, BuiltinDefaultID)
	}

	in := Inbox("BIL")
	if err := Validate(&in); err != nil {
		t.Fatalf("inbox invalid: %v", err)
	}
	if in.ID != BuiltinInboxID {
		t.Fatalf("id = %q", in.ID)
	}

	if !IsBuiltinID(BuiltinDefaultID) || !IsBuiltinID(BuiltinInboxID) {
		t.Fatal("IsBuiltinID broken")
	}
	if IsBuiltinID("user-uuid") {
		t.Fatal("user id mistakenly built-in")
	}
}

func TestBuiltins_DefaultBuckets(t *testing.T) {
	def := Default("BIL")
	mk := func(labels string) fakeIssue { return fakeIssue{"labels": labels} }
	out := Apply([]Issue{mk("blocker"), mk("")}, &def)

	titles := make([]string, len(out))
	for i := range out {
		titles[i] = out[i].Title
	}
	want := []string{"Projects", "Entry"}
	if len(titles) != len(want) {
		t.Fatalf("titles = %#v, want %#v", titles, want)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("titles[%d] = %q, want %q", i, titles[i], want[i])
		}
	}
}
