package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FieldErrors extracts per-field validation errors and the joined top-level
// errorMessages from a 400 response returned by the Jira REST API. The
// caller does not need to know about the underlying HTTP error type.
//
// Returns (nil, "", false) when err is not a 400 from the API or when the
// body is not in the expected `{"errorMessages":[…],"errors":{…}}` shape.
func FieldErrors(err error) (fields map[string]string, generic string, ok bool) {
	var he *HTTPError
	if !errors.As(err, &he) || he.StatusCode != http.StatusBadRequest {
		return nil, "", false
	}
	var body struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	if jsonErr := json.Unmarshal(he.Body, &body); jsonErr != nil {
		return nil, "", false
	}
	return body.Errors, strings.Join(body.ErrorMessages, "; "), true
}

// DefaultTimeout is the per-request timeout used by NewClient.
const DefaultTimeout = 30 * time.Second

// MaxRetries is the number of retry attempts on top of the initial request
// (so the total attempt count is MaxRetries+1).
const MaxRetries = 2

// EnvDebug toggles debug logging when set to "1".
const EnvDebug = "RIPJIRA_DEBUG"

// Version identifies the ripjira build sent in the User-Agent header.
// cmd/ripjira overrides this at startup with the binary's version string.
var Version = "dev"

// UserAgent returns the value sent in the User-Agent header on every Jira
// request. It identifies the client to Atlassian and to any reverse proxy
// in the path so administrators can attribute traffic.
func UserAgent() string {
	return fmt.Sprintf("ripjira/%s (%s/%s; +https://github.com/billygate/ripjira)",
		Version, runtime.GOOS, runtime.GOARCH)
}

// HTTPError is returned when Jira responds with a non-2xx status that is not
// retried (typically 4xx). It exposes the status code and raw body for
// callers that need to surface field-level errors (e.g. 400 from createIssue).
type HTTPError struct {
	StatusCode int
	Status     string
	Body       []byte
	Method     string
	URL        string
}

// Error returns a short message including status and a body excerpt.
func (e *HTTPError) Error() string {
	body := strings.TrimSpace(string(e.Body))
	if len(body) > 256 {
		body = body[:256] + "…"
	}
	if body == "" {
		return fmt.Sprintf("jira: %s %s -> %d %s", e.Method, e.URL, e.StatusCode, e.Status)
	}
	return fmt.Sprintf("jira: %s %s -> %d %s: %s", e.Method, e.URL, e.StatusCode, e.Status, body)
}

// Client is a thin wrapper around net/http for the Jira Cloud REST API v3.
type Client struct {
	baseURL    *url.URL
	email      string
	token      string
	authHeader string
	http       *http.Client

	// backoff returns the wait duration for retry attempt n (0-indexed). It is
	// exposed for tests; production code uses the default exponential schedule.
	backoff func(attempt int) time.Duration

	// now is overridable in tests so Retry-After can be exercised without
	// real sleeps. clock advances when sleep is called.
	now   func() time.Time
	sleep func(context.Context, time.Duration) error

	// debugLogPath is resolved lazily on first debug-eligible call; empty
	// when debug logging is disabled.
	debugMu       sync.Mutex
	debugFile     *os.File
	debugLogPath  string
	debugResolved bool

	mu        sync.Mutex
	accountID string

	// extraFields is appended to the search field list so configured Jira
	// customfield_<id> values come back populated in Issue.CustomFields.
	extraFields []string
}

// SetExtraFields configures additional Jira field IDs the client will
// request alongside the default summary/status/etc set. Typically callers
// pass `customfield_XXXXX` IDs sourced from config.yaml.
func (c *Client) SetExtraFields(ids []string) {
	c.extraFields = append([]string(nil), ids...)
}

// NewClient validates baseURL and returns a ready-to-use Client.
func NewClient(baseURL, email, token string) (*Client, error) {
	if baseURL == "" {
		return nil, errors.New("jira: base URL is required")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("jira: parse base URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("jira: base URL must include scheme and host: %q", baseURL)
	}
	if email == "" {
		return nil, errors.New("jira: email is required")
	}
	if token == "" {
		return nil, errors.New("jira: token is required")
	}
	auth := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
	return &Client{
		baseURL:    u,
		email:      email,
		token:      token,
		authHeader: "Basic " + auth,
		http:       &http.Client{Timeout: DefaultTimeout},
		backoff:    defaultBackoff,
		now:        time.Now,
		sleep:      sleepCtx,
	}, nil
}

// BaseURL returns the parsed base URL.
func (c *Client) BaseURL() *url.URL { return c.baseURL }

// AccountID returns the cached account ID, if any.
func (c *Client) AccountID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accountID
}

// SetAccountID caches the current user's account ID.
func (c *Client) SetAccountID(id string) {
	c.mu.Lock()
	c.accountID = id
	c.mu.Unlock()
}

// do issues a request to path (joined onto baseURL), JSON-encoding body and
// JSON-decoding the response into out. body and out may be nil. The retry
// policy is documented on MaxRetries.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("jira: encode body: %w", err)
		}
	}

	reqURL, err := c.resolve(path)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		var bodyReader io.Reader
		if raw != nil {
			bodyReader = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
		if err != nil {
			return fmt.Errorf("jira: build request: %w", err)
		}
		req.Header.Set("Authorization", c.authHeader)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", UserAgent())
		if raw != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		c.debugLogRequest(req, raw)

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if attempt == MaxRetries {
				return fmt.Errorf("jira: %s %s: %w", method, reqURL, err)
			}
			if waitErr := c.sleep(ctx, c.backoff(attempt)); waitErr != nil {
				return waitErr
			}
			continue
		}

		// Read body up front so we can include it in errors and debug logs.
		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		c.debugLogResponse(req, resp, respBody)
		if readErr != nil {
			lastErr = readErr
			if attempt == MaxRetries {
				return fmt.Errorf("jira: read response: %w", readErr)
			}
			if waitErr := c.sleep(ctx, c.backoff(attempt)); waitErr != nil {
				return waitErr
			}
			continue
		}

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			if out != nil && len(respBody) > 0 {
				if err := json.Unmarshal(respBody, out); err != nil {
					return fmt.Errorf("jira: decode response: %w", err)
				}
			}
			return nil

		case resp.StatusCode == http.StatusTooManyRequests:
			if attempt == MaxRetries {
				return &HTTPError{
					StatusCode: resp.StatusCode,
					Status:     resp.Status,
					Body:       respBody,
					Method:     method,
					URL:        reqURL,
				}
			}
			wait := parseRetryAfter(resp.Header.Get("Retry-After"), c.now)
			if wait <= 0 {
				wait = c.backoff(attempt)
			}
			if waitErr := c.sleep(ctx, wait); waitErr != nil {
				return waitErr
			}
			continue

		case resp.StatusCode >= 500:
			lastErr = &HTTPError{
				StatusCode: resp.StatusCode,
				Status:     resp.Status,
				Body:       respBody,
				Method:     method,
				URL:        reqURL,
			}
			if attempt == MaxRetries {
				return lastErr
			}
			if waitErr := c.sleep(ctx, c.backoff(attempt)); waitErr != nil {
				return waitErr
			}
			continue

		default:
			return &HTTPError{
				StatusCode: resp.StatusCode,
				Status:     resp.Status,
				Body:       respBody,
				Method:     method,
				URL:        reqURL,
			}
		}
	}

	if lastErr != nil {
		return lastErr
	}
	return errors.New("jira: request failed without error (this should not happen)")
}

// Do is the exported wrapper around do for use by sibling files in this
// package. Keeping the lowercase do unexported communicates that callers
// outside the package must use higher-level methods (MyIssues, GetIssue, …).
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	return c.do(ctx, method, path, body, out)
}

func (c *Client) resolve(path string) (string, error) {
	if path == "" {
		return "", errors.New("jira: empty request path")
	}
	// Allow callers to pass either an absolute path ("/rest/api/3/...") or a
	// path relative to baseURL.
	ref, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("jira: parse path %q: %w", path, err)
	}
	resolved := c.baseURL.ResolveReference(ref)
	return resolved.String(), nil
}

func defaultBackoff(attempt int) time.Duration {
	// 200ms, 400ms, 800ms, … capped at 5s.
	d := time.Duration(200*(1<<attempt)) * time.Millisecond
	return min(d, 5*time.Second)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func parseRetryAfter(h string, now func() time.Time) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		d := t.Sub(now())
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

// --- debug logging ----------------------------------------------------------

func (c *Client) debugEnabled() bool {
	return os.Getenv(EnvDebug) == "1"
}

func (c *Client) ensureDebugFile() *os.File {
	c.debugMu.Lock()
	defer c.debugMu.Unlock()
	if c.debugResolved {
		return c.debugFile
	}
	c.debugResolved = true

	path := c.debugLogPath
	if path == "" {
		var err error
		path, err = defaultDebugLogPath()
		if err != nil {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	c.debugFile = f
	return f
}

// SetDebugLogPath overrides the debug log destination. Callers that pass an
// empty string restore the default ($XDG_CACHE_HOME or ~/.cache).
func (c *Client) SetDebugLogPath(path string) {
	c.debugMu.Lock()
	defer c.debugMu.Unlock()
	if c.debugFile != nil {
		_ = c.debugFile.Close()
		c.debugFile = nil
	}
	c.debugLogPath = path
	c.debugResolved = false
}

// CloseDebugLog flushes and closes the debug log file if open.
func (c *Client) CloseDebugLog() error {
	c.debugMu.Lock()
	defer c.debugMu.Unlock()
	if c.debugFile == nil {
		return nil
	}
	err := c.debugFile.Close()
	c.debugFile = nil
	c.debugResolved = false
	return err
}

func (c *Client) debugLogRequest(req *http.Request, body []byte) {
	if !c.debugEnabled() {
		return
	}
	f := c.ensureDebugFile()
	if f == nil {
		return
	}
	auth := req.Header.Get("Authorization")
	if auth != "" {
		req.Header.Set("Authorization", "***")
		defer req.Header.Set("Authorization", auth)
	}
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = fmt.Fprintf(f, "%s --> %s %s\n", ts, req.Method, req.URL.String())
	for k, v := range req.Header {
		val := strings.Join(v, ",")
		if strings.EqualFold(k, "Authorization") {
			val = "***"
		}
		_, _ = fmt.Fprintf(f, "%s     %s: %s\n", ts, k, val)
	}
	if len(body) > 0 {
		_, _ = fmt.Fprintf(f, "%s     body: %s\n", ts, truncate(body, 1024))
	}
}

func (c *Client) debugLogResponse(req *http.Request, resp *http.Response, body []byte) {
	if !c.debugEnabled() {
		return
	}
	f := c.ensureDebugFile()
	if f == nil {
		return
	}
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = fmt.Fprintf(f, "%s <-- %s %s %d %s\n", ts, req.Method, req.URL.String(), resp.StatusCode, resp.Status)
	if len(body) > 0 {
		_, _ = fmt.Fprintf(f, "%s     body: %s\n", ts, truncate(body, 1024))
	}
}

func truncate(b []byte, n int) string {
	s := strings.ReplaceAll(string(b), "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func defaultDebugLogPath() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "ripjira", "debug.log"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "ripjira", "debug.log"), nil
}
