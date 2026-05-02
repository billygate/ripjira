package jira

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient builds a Client pointed at srv with a backoff that does not
// sleep, so retry tests run instantly.
func newTestClient(t *testing.T, srv *httptest.Server, email, token string) *Client {
	t.Helper()
	c, err := NewClient(srv.URL, email, token)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.backoff = func(int) time.Duration { return 0 }
	c.sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	return c
}

func TestNewClient_Validation(t *testing.T) {
	cases := []struct {
		name             string
		base, email, tok string
		wantErr          bool
	}{
		{"ok", "https://acme.atlassian.net", "a@b.com", "tok", false},
		{"empty base", "", "a@b.com", "tok", true},
		{"no scheme", "acme.atlassian.net", "a@b.com", "tok", true},
		{"missing email", "https://acme.atlassian.net", "", "tok", true},
		{"missing token", "https://acme.atlassian.net", "a@b.com", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClient(tc.base, tc.email, tc.tok)
			if (err != nil) != tc.wantErr {
				t.Fatalf("got err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestDo_AuthHeaderAndJSONRoundTrip(t *testing.T) {
	type echo struct {
		Hello string `json:"hello"`
	}
	var gotAuth, gotMethod, gotPath, gotCT, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hello":"world"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "secret")

	var out echo
	if err := c.do(context.Background(), http.MethodPost, "/rest/api/3/issue", echo{Hello: "ping"}, &out); err != nil {
		t.Fatalf("do: %v", err)
	}

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("a@b.com:secret"))
	if gotAuth != wantAuth {
		t.Fatalf("auth header: got %q, want %q", gotAuth, wantAuth)
	}
	if gotMethod != http.MethodPost || gotPath != "/rest/api/3/issue" {
		t.Fatalf("method/path: got %s %s", gotMethod, gotPath)
	}
	if gotCT != "application/json" {
		t.Fatalf("Content-Type: got %q", gotCT)
	}
	if gotAccept != "application/json" {
		t.Fatalf("Accept: got %q", gotAccept)
	}
	if out.Hello != "world" {
		t.Fatalf("decoded body: got %+v", out)
	}
}

func TestDo_NoBodyOrOut(t *testing.T) {
	var saw bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw = true
		if r.Header.Get("Content-Type") != "" {
			t.Errorf("Content-Type set on bodyless request: %q", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if err := c.do(context.Background(), http.MethodDelete, "/x", nil, nil); err != nil {
		t.Fatalf("do: %v", err)
	}
	if !saw {
		t.Fatal("server never received request")
	}
}

func TestDo_5xxRetriedTwiceThenSurfaced(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, `{"err":"boom"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	err := c.do(context.Background(), http.MethodGet, "/x", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var herr *HTTPError
	if !errors.As(err, &herr) || herr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected HTTPError 500, got %T %v", err, err)
	}
	if got := atomic.LoadInt32(&hits); got != MaxRetries+1 {
		t.Fatalf("attempt count: got %d, want %d", got, MaxRetries+1)
	}
}

func TestDo_5xxThenSuccess(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n < 3 {
			http.Error(w, "transient", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if err := c.do(context.Background(), http.MethodGet, "/x", nil, &struct{}{}); err != nil {
		t.Fatalf("do: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("attempts: got %d, want 3", got)
	}
}

func TestDo_429HonorsRetryAfter(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "7")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	var slept time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		slept = d
		return nil
	}

	if err := c.do(context.Background(), http.MethodGet, "/x", nil, &struct{}{}); err != nil {
		t.Fatalf("do: %v", err)
	}
	if slept != 7*time.Second {
		t.Fatalf("slept: got %v, want 7s (Retry-After honored)", slept)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("attempts: got %d, want 2", got)
	}
}

func TestDo_4xxSurfacedAsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":{"summary":"required"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	err := c.do(context.Background(), http.MethodPost, "/x", map[string]string{"x": "y"}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var herr *HTTPError
	if !errors.As(err, &herr) {
		t.Fatalf("expected *HTTPError, got %T %v", err, err)
	}
	if herr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", herr.StatusCode)
	}
	if !strings.Contains(string(herr.Body), `"summary":"required"`) {
		t.Fatalf("body not preserved: %q", string(herr.Body))
	}
	// 4xx must not be retried.
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("error message: %q", err.Error())
	}
}

func TestDo_ContextCancelAborts(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	c := newTestClient(t, srv, "a@b.com", "tok")
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.do(ctx, http.MethodGet, "/x", nil, nil)
	}()
	<-started
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("do did not return after context cancel")
	}
}

func TestDo_NetworkErrorRetried(t *testing.T) {
	// Server is closed immediately so dialing fails.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	addr := srv.URL
	srv.Close()

	c, err := NewClient(addr, "a@b.com", "tok")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	var attempts int32
	c.backoff = func(int) time.Duration { return 0 }
	c.sleep = func(_ context.Context, _ time.Duration) error {
		atomic.AddInt32(&attempts, 1)
		return nil
	}

	if err := c.do(context.Background(), http.MethodGet, "/x", nil, nil); err == nil {
		t.Fatal("expected error reaching closed server")
	}
	// Two retries means sleep is called twice (between attempts 1→2 and 2→3).
	if got := atomic.LoadInt32(&attempts); got != MaxRetries {
		t.Fatalf("retry sleeps: got %d, want %d", got, MaxRetries)
	}
}

func TestDo_ResolvesAbsolutePath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/myself" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, "a@b.com", "tok")
	if err := c.do(context.Background(), http.MethodGet, "/rest/api/3/myself", nil, &struct{}{}); err != nil {
		t.Fatalf("do: %v", err)
	}
}

func TestDo_DebugLogMasksAuthorization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "debug.log")
	t.Setenv(EnvDebug, "1")

	c := newTestClient(t, srv, "a@b.com", "supersecret")
	c.SetDebugLogPath(logPath)
	t.Cleanup(func() { _ = c.CloseDebugLog() })

	if err := c.do(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatalf("do: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logged := string(data)
	if logged == "" {
		t.Fatal("debug log is empty")
	}
	if strings.Contains(logged, "supersecret") {
		t.Fatalf("debug log leaked token: %q", logged)
	}
	wantAuth := base64.StdEncoding.EncodeToString([]byte("a@b.com:supersecret"))
	if strings.Contains(logged, wantAuth) {
		t.Fatalf("debug log leaked Basic-encoded credentials")
	}
	if !strings.Contains(logged, "Authorization") {
		t.Fatalf("Authorization header not logged at all: %q", logged)
	}
	if !strings.Contains(logged, "***") {
		t.Fatalf("Authorization not masked with ***: %q", logged)
	}
}

func TestDo_DebugLogDisabledByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "debug.log")
	// EnvDebug intentionally not set.
	t.Setenv(EnvDebug, "")

	c := newTestClient(t, srv, "a@b.com", "tok")
	c.SetDebugLogPath(logPath)

	if err := c.do(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatalf("do: %v", err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("debug log written despite RIPJIRA_DEBUG unset (stat err=%v)", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		header string
		want   time.Duration
	}{
		{"", 0},
		{"5", 5 * time.Second},
		{"-3", 0},
		{"garbage", 0},
		{now.Add(10 * time.Second).UTC().Format(http.TimeFormat), 10 * time.Second},
		{now.Add(-1 * time.Hour).UTC().Format(http.TimeFormat), 0},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.header), func(t *testing.T) {
			got := parseRetryAfter(tc.header, func() time.Time { return now })
			// Allow ±1s tolerance for HTTP-date second rounding.
			diff := got - tc.want
			if diff < -time.Second || diff > time.Second {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAccountIDCache(t *testing.T) {
	c, err := NewClient("https://acme.atlassian.net", "a@b.com", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if c.AccountID() != "" {
		t.Fatalf("expected empty cache, got %q", c.AccountID())
	}
	c.SetAccountID("abc-123")
	if got := c.AccountID(); got != "abc-123" {
		t.Fatalf("AccountID: got %q", got)
	}
}
