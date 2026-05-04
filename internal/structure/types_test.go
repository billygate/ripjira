package structure

import (
	"encoding/json"
	"reflect"
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
