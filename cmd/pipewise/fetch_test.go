package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testToken = "glpat-do-not-print-me"

// fakeGitLab answers 200 with body on the CI lint endpoint when the token
// is right, 401 otherwise, and points GITLAB_URL and GITLAB_TOKEN at it.
// Other error statuses are tested on the client, in internal/gitlab.
func fakeGitLab(t *testing.T, body string) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != testToken || !strings.HasPrefix(r.Header.Get("User-Agent"), "pipewise/") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	t.Setenv("GITLAB_URL", s.URL)
	t.Setenv("GITLAB_TOKEN", testToken)
}

func noToken(t *testing.T, r result) {
	t.Helper()
	if strings.Contains(r.stdout+r.stderr, testToken) {
		t.Errorf("the token was printed:\n%s\n%s", r.stdout, r.stderr)
	}
}

func TestFetch(t *testing.T) {
	fakeGitLab(t, `{"valid": true, "errors": [], "warnings": ["uses only"], "merged_yaml": "lint:\n  script: [x]"}`)
	r := pipewise(t, "", "fetch", "--project", "group/app")
	if r.code != exitOK || r.stdout != "lint:\n  script: [x]\n" {
		t.Errorf("fetch: %+v", r)
	}
	if r.stderr != "pipewise: GitLab warning: uses only\n" {
		t.Errorf("stderr = %q", r.stderr)
	}
	noToken(t, r)
}

func TestFetchToFile(t *testing.T) {
	fakeGitLab(t, `{"valid": true, "merged_yaml": "lint:\n  script: [x]\n"}`)
	path := filepath.Join(t.TempDir(), "merged.yml")
	r := pipewise(t, "", "fetch", "--project", "42", "--ref", "v1.0.0", "-o", path)
	data, err := os.ReadFile(path)
	if r.code != exitOK || r.stdout != "" || err != nil || string(data) != "lint:\n  script: [x]\n" {
		t.Errorf("fetch -o: %+v, file %q, %v", r, data, err)
	}

	r = pipewise(t, "", "fetch", "--project", "42", "-o", filepath.Join(t.TempDir(), "missing", "dir", "x.yml"))
	if r.code != exitError || !strings.Contains(r.stderr, "no such file or directory") {
		t.Errorf("unwritable -o: %+v", r)
	}
}

func TestFetchInvalidConfiguration(t *testing.T) {
	fakeGitLab(t, `{"valid": false, "errors": ["jobs:lint script config should be a string", "stage x is not defined"], "merged_yaml": "lint: {}"}`)
	r := pipewise(t, "", "fetch", "--project", "group/app")
	want := "pipewise: GitLab reports the configuration as invalid:\n" +
		"  - jobs:lint script config should be a string\n  - stage x is not defined\n"
	if r.code != exitFindings || r.stdout != "" || r.stderr != want {
		t.Errorf("invalid configuration: %+v", r)
	}
}

func TestFetchErrors(t *testing.T) {
	fakeGitLab(t, `{"valid": true}`)
	tests := []struct {
		name   string
		env    map[string]string
		args   []string
		stderr string
	}{
		{"no project", nil, []string{"fetch"}, "pipewise: --project is required\n"},
		{"positional argument", nil, []string{"fetch", "group/app"}, "pipewise: unknown command \"group/app\" for \"pipewise fetch\"\n"},
		{"bad GITLAB_URL", map[string]string{"GITLAB_URL": "gitlab.com"}, []string{"fetch", "--project", "a/b"},
			"pipewise: GITLAB_URL must be an http or https URL, not \"gitlab.com\"\n"},
		{"wrong token", map[string]string{"GITLAB_TOKEN": "glpat-wrong-" + testToken}, []string{"fetch", "--project", "a/b"},
			"pipewise: GitLab answered 401: check GITLAB_TOKEN: it is missing, expired, or cannot read this project's CI configuration\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			r := pipewise(t, "", tt.args...)
			if r.code != exitError || r.stderr != tt.stderr || r.stdout != "" {
				t.Errorf("got %+v\nwant stderr %q", r, tt.stderr)
			}
			noToken(t, r)
		})
	}
}

func TestGitLabURL(t *testing.T) {
	t.Setenv("GITLAB_URL", "")
	if got := gitlabURL(); got != "https://gitlab.com" {
		t.Errorf("default = %q", got)
	}
	t.Setenv("GITLAB_URL", "https://gitlab.example")
	if got := gitlabURL(); got != "https://gitlab.example" {
		t.Errorf("from GITLAB_URL = %q", got)
	}
}

func TestFetchStdoutError(t *testing.T) {
	fakeGitLab(t, `{"valid": true, "merged_yaml": "lint: {}\n"}`)
	var errb strings.Builder
	code := run([]string{"fetch", "--project", "a/b"}, strings.NewReader(""), failingIO{}, &errb)
	if code != exitError || !strings.Contains(errb.String(), "write the configuration") {
		t.Errorf("exit %d, stderr %q", code, errb.String())
	}
}

func TestFetchThenLint(t *testing.T) {
	fakeGitLab(t, `{"valid": true, "merged_yaml": "lint:\n  script: [x]\n  rules: [{when: on_success}]\n"}`)
	fetched := pipewise(t, "", "fetch", "--project", "group/app")
	linted := pipewise(t, fetched.stdout, "lint", "-")
	if linted.code != exitFindings || !strings.Contains(linted.stdout, "<stdin>:1: error redundant-job") {
		t.Errorf("fetch | lint -: %+v", linted)
	}
}
