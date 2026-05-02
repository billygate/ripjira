package jira

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAddComment_PostsADFBody(t *testing.T) {
	var (
		gotMethod, gotPath, gotCT string
		gotBody                   []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"100"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if err := c.AddComment(context.Background(), "PROJ-1", "first line\nsecond line\n\nnext para"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method: got %q want POST", gotMethod)
	}
	if gotPath != "/rest/api/3/issue/PROJ-1/comment" {
		t.Fatalf("path: got %q", gotPath)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Fatalf("content-type: got %q", gotCT)
	}

	var payload struct {
		Body ADF `json:"body"`
	}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("decode body: %v\nraw: %s", err, gotBody)
	}
	if payload.Body.Type != "doc" || payload.Body.Version != 1 {
		t.Fatalf("doc envelope: got %+v", payload.Body)
	}
	if len(payload.Body.Content) != 2 {
		t.Fatalf("paragraph count: got %d want 2", len(payload.Body.Content))
	}
	first := payload.Body.Content[0]
	if first.Type != "paragraph" || len(first.Content) != 3 {
		t.Fatalf("first paragraph: %+v", first)
	}
	if first.Content[0].Type != "text" || first.Content[0].Text != "first line" {
		t.Fatalf("first text: %+v", first.Content[0])
	}
	if first.Content[1].Type != "hardBreak" {
		t.Fatalf("expected hardBreak between lines: %+v", first.Content[1])
	}
	if first.Content[2].Type != "text" || first.Content[2].Text != "second line" {
		t.Fatalf("second text: %+v", first.Content[2])
	}
	second := payload.Body.Content[1]
	if second.Type != "paragraph" || len(second.Content) != 1 || second.Content[0].Text != "next para" {
		t.Fatalf("second paragraph: %+v", second)
	}
}

func TestAddComment_RejectsEmptyKey(t *testing.T) {
	c, err := NewClient("https://example.atlassian.net", "a@b.com", "tok")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := c.AddComment(context.Background(), "", "hello"); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestAddComment_PropagatesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["nope"]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	err := c.AddComment(context.Background(), "PROJ-2", "body")
	if err == nil {
		t.Fatal("expected error")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected HTTPError 400, got %v", err)
	}
}
