package structure

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestFilterClause_JSONShorthand(t *testing.T) {
	var c FilterClause
	if err := json.Unmarshal([]byte(`["High","Medium"]`), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(c.In, []string{"High", "Medium"}) {
		t.Fatalf("want In=[High Medium], got %#v", c)
	}

	yes := true
	var d FilterClause
	in := []byte(`{"in":["a"],"not":["b"],"regex":"^x","contains":"y","exists":true}`)
	if err := json.Unmarshal(in, &d); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}
	want := FilterClause{In: []string{"a"}, Not: []string{"b"}, Regex: "^x", Contains: "y", Exists: &yes}
	if !reflect.DeepEqual(d.In, want.In) || !reflect.DeepEqual(d.Not, want.Not) ||
		d.Regex != want.Regex || d.Contains != want.Contains ||
		d.Exists == nil || *d.Exists != *want.Exists {
		t.Fatalf("object form mismatch: %#v", d)
	}

	out, err := json.Marshal(FilterClause{In: []string{"X"}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != `["X"]` {
		t.Fatalf("want array shorthand, got %s", out)
	}

	out, err = json.Marshal(FilterClause{In: []string{"X"}, Regex: "^x"})
	if err != nil {
		t.Fatalf("marshal mixed: %v", err)
	}
	if string(out) != `{"in":["X"],"regex":"^x"}` {
		t.Fatalf("want object form, got %s", out)
	}
}

func TestFilterClause_YAMLShorthand(t *testing.T) {
	src := []byte(`
title: T
filter:
  status: [Open, "In Progress"]
  assignee:
    exists: true
`)
	var s Section
	if err := yaml.Unmarshal(src, &s); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	if got := s.Filter["status"].In; !reflect.DeepEqual(got, []string{"Open", "In Progress"}) {
		t.Fatalf("status In = %#v", got)
	}
	if e := s.Filter["assignee"].Exists; e == nil || !*e {
		t.Fatalf("assignee.exists should be true, got %#v", e)
	}
}

func TestStructure_YAMLRoundTrip_WithScope(t *testing.T) {
	in := Structure{
		ID:   "s1",
		Name: "n",
		Scope: SectionFilter{
			"labels": {In: []string{"Q12026", "Q22026"}},
		},
		Sections: []Section{{Title: "T"}},
	}
	out, err := yaml.Marshal(&in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "scope:") {
		t.Fatalf("expected scope in YAML, got:\n%s", out)
	}
	var got Structure
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got.Scope, in.Scope) {
		t.Fatalf("scope round-trip mismatch:\nwant %#v\ngot  %#v", in.Scope, got.Scope)
	}
}

func TestStructure_YAMLRoundTrip_EmptyScopeOmitted(t *testing.T) {
	in := Structure{ID: "s1", Name: "n", Sections: []Section{{Title: "T"}}}
	out, err := yaml.Marshal(&in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "scope:") {
		t.Fatalf("expected scope omitted, got:\n%s", out)
	}
}
