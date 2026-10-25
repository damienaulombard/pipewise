package gitlab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const token = "glpat-secret-token-0123456789"

type request struct {
	path, query, token, userAgent string
}

// server answers every request with status and body, and records it.
func server(t *testing.T, status int, body string) (*Client, *request) {
	t.Helper()
	got := &request{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = request{r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("PRIVATE-TOKEN"), r.Header.Get("User-Agent")}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return &Client{BaseURL: s.URL + "/", Token: token, UserAgent: "pipewise/test", HTTP: s.Client()}, got
}

func TestMergedConfig(t *testing.T) {
	c, got := server(t, http.StatusOK,
		`{"valid": true, "errors": [], "warnings": ["jobs:x uses only"], "merged_yaml": "lint:\n  script: [x]\n"}`)
	lint, err := c.MergedConfig(context.Background(), "group/sub/app", "release/1.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/v4/projects/group%2Fsub%2Fapp/ci/lint" {
		t.Errorf("path = %s", got.path)
	}
	if got.query != "content_ref=release%2F1.0" {
		t.Errorf("query = %s", got.query)
	}
	if got.token != token || got.userAgent != "pipewise/test" {
		t.Errorf("headers: token %q, user agent %q", got.token, got.userAgent)
	}
	if !lint.Valid || lint.MergedYAML != "lint:\n  script: [x]\n" || len(lint.Warnings) != 1 {
		t.Errorf("lint = %+v", lint)
	}
}

func TestMergedConfigDefaults(t *testing.T) {
	c, got := server(t, http.StatusOK, `{"valid": true, "merged_yaml": ""}`)
	c.Token = ""
	if _, err := c.MergedConfig(context.Background(), "42", ""); err != nil {
		t.Fatal(err)
	}
	// A numeric id as is, no content_ref, no token header.
	if got.path != "/api/v4/projects/42/ci/lint" || got.query != "" || got.token != "" {
		t.Errorf("request = %+v", got)
	}
}

func TestMergedConfigErrors(t *testing.T) {
	tests := []struct {
		status int
		body   string
		want   string
	}{
		{401, `{"message": "401 Unauthorized"}`, "GitLab answered 401: check GITLAB_TOKEN: it is missing, expired, or cannot read this project's CI configuration (401 Unauthorized)"},
		{403, `{"message": "403 Forbidden"}`, "GitLab answered 403: check GITLAB_TOKEN"},
		{404, `{"message": "404 Project Not Found"}`, "GitLab answered 404: the project or the ref does not exist, or the token cannot see it (404 Project Not Found)"},
		{500, `<html>oops</html>`, "GitLab answered 500: unexpected answer"},
		{502, `{"message": {"base": ["upstream"]}}`, "GitLab answered 502: unexpected answer (map[base:[upstream]])"},
		{200, `not json`, "read GitLab's answer"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			c, _ := server(t, tt.status, tt.body)
			_, err := c.MergedConfig(context.Background(), "group/app", "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if strings.Contains(err.Error(), token) {
				t.Error("the token appears in the error")
			}
			var se *StatusError
			if tt.status != 200 && (!errors.As(err, &se) || se.Status != tt.status) {
				t.Errorf("error is not a StatusError %d: %v", tt.status, err)
			}
		})
	}
}

func TestMergedConfigUnreachable(t *testing.T) {
	c, _ := server(t, 200, "")
	s := httptest.NewServer(http.NotFoundHandler())
	s.Close() // nothing listens there any more
	c.BaseURL = s.URL
	_, err := c.MergedConfig(context.Background(), "group/app", "")
	if err == nil || !strings.HasPrefix(err.Error(), "reach GitLab:") || strings.Contains(err.Error(), token) {
		t.Errorf("error = %v", err)
	}
}

func TestMergedConfigBadURL(t *testing.T) {
	c := &Client{BaseURL: "http://bad host", HTTP: http.DefaultClient}
	if _, err := c.MergedConfig(context.Background(), "group/app", ""); err == nil || !strings.HasPrefix(err.Error(), "build the request:") {
		t.Errorf("error = %v", err)
	}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("connection reset") }
func (failingBody) Close() error             { return nil }

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: failingBody{}, Header: http.Header{}}, nil
}

func TestMergedConfigBodyError(t *testing.T) {
	c := &Client{BaseURL: "https://gitlab.example", HTTP: &http.Client{Transport: failingTransport{}}}
	if _, err := c.MergedConfig(context.Background(), "group/app", ""); err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("error = %v", err)
	}
}
