package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

type result struct {
	code           int
	stdout, stderr string
}

func pipewise(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
	return result{code, out.String(), errb.String()}
}

func fixture(name string) string {
	return filepath.Join("..", "..", "testdata", "report", name)
}

// writeConfig writes a configuration to a temporary file.
func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".gitlab-ci.yml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCurrentVersion(t *testing.T) {
	savedVersion, savedRead := version, readBuildInfo
	t.Cleanup(func() { version, readBuildInfo = savedVersion, savedRead })
	info := func(v string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Path: "github.com/damienaulombard/pipewise", Version: v}}, true
		}
	}
	tests := []struct {
		name     string
		injected string
		read     func() (*debug.BuildInfo, bool)
		want     string
	}{
		{"injected at build time", "v0.1.0", info("v0.0.9"), "v0.1.0"},
		{"go install of a tag", "dev", info("v0.1.0"), "v0.1.0"},
		{"go install of a commit", "dev", info("v0.0.0-20261025174543-1e18496dd55a"), "v0.0.0-20261025174543-1e18496dd55a"},
		{"built from a checkout", "dev", info("(devel)"), "dev"},
		{"no build information", "dev", func() (*debug.BuildInfo, bool) { return nil, false }, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, readBuildInfo = tt.injected, tt.read
			if got := currentVersion(); got != tt.want {
				t.Errorf("currentVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionCommand(t *testing.T) {
	saved := version
	t.Cleanup(func() { version = saved })
	for _, v := range []string{"dev", "v0.1.0"} {
		version = v
		r := pipewise(t, "", "version")
		if r.code != exitOK || r.stdout != "pipewise "+v+"\n" {
			t.Errorf("version %s: %+v", v, r)
		}
	}
	if r := pipewise(t, "", "version", "extra"); r.code != exitError {
		t.Errorf("version with an argument: %+v", r)
	}
}

// infoOnly has a single info finding: missing-interruptible.
const infoOnly = `unit:
  image: golang:1.26@sha256:0123456789abcdef
  script: [go test ./...]
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
`

func TestLintExitCodes(t *testing.T) {
	clean, all, info := fixture("clean.gitlab-ci.yml"), fixture("all-rules.gitlab-ci.yml"), writeConfig(t, infoOnly)
	tests := []struct {
		name string
		args []string
		code int
	}{
		{"clean file", []string{"lint", clean}, exitOK},
		{"an error finding fails by default", []string{"lint", all}, exitFindings},
		{"--fail-on none never fails", []string{"lint", "--fail-on", "none", all}, exitOK},
		{"an info finding passes by default", []string{"lint", info}, exitOK},
		{"an info finding passes --fail-on warning", []string{"lint", "--fail-on", "warning", info}, exitOK},
		{"an info finding fails --fail-on info", []string{"lint", "--fail-on", "info", info}, exitFindings},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := pipewise(t, "", tt.args...)
			if r.code != tt.code {
				t.Errorf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", r.code, tt.code, r.stdout, r.stderr)
			}
			if r.stderr != "" {
				t.Errorf("stderr = %q, want nothing", r.stderr)
			}
		})
	}
}

func TestLintOutput(t *testing.T) {
	all := fixture("all-rules.gitlab-ci.yml")

	text := pipewise(t, "", "lint", all)
	if !strings.Contains(text.stdout, all+":8: error redundant-job") ||
		!strings.HasSuffix(text.stdout, all+": 8 findings: 1 error, 3 warnings, 4 info.\n") {
		t.Errorf("text report:\n%s", text.stdout)
	}

	js := pipewise(t, "", "lint", "--format", "json", all)
	var report struct {
		Schema   int    `json:"schema"`
		File     string `json:"file"`
		Findings []any  `json:"findings"`
	}
	if err := json.Unmarshal([]byte(js.stdout), &report); err != nil {
		t.Fatalf("JSON report: %v\n%s", err, js.stdout)
	}
	if report.Schema != 1 || report.File != all || len(report.Findings) != 8 {
		t.Errorf("JSON report = %+v", report)
	}
}

func TestLintStdin(t *testing.T) {
	r := pipewise(t, infoOnly, "lint", "-")
	if r.code != exitOK || !strings.Contains(r.stdout, "<stdin>:1: info missing-interruptible") {
		t.Errorf("stdin: %+v", r)
	}
}

func TestLintOptions(t *testing.T) {
	// The job runs on merge requests and on a branch named main.
	mainOnly := writeConfig(t, `unit:
  interruptible: true
  image: golang:1.26@sha256:0123456789abcdef
  script: [x]
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - if: $CI_COMMIT_BRANCH == "main"
`)
	if r := pipewise(t, "", "lint", mainOnly); r.code != exitFindings {
		t.Errorf("default branch main: exit %d, want a redundant job\n%s", r.code, r.stdout)
	}
	if r := pipewise(t, "", "lint", "--default-branch", "master", mainOnly); r.code != exitOK {
		t.Errorf("default branch master: exit %d\n%s", r.code, r.stdout)
	}

	// Without rules, whether a job runs on merge requests depends on the
	// project setting.
	noRules := writeConfig(t, "lint:\n  script: [x]\n")
	for setting, want := range map[string]string{"true": "runs", "false": "skipped", "unknown": "maybe"} {
		r := pipewise(t, "", "lint", "--format", "json", "--skip-branch-pipelines-for-mrs", setting, noRules)
		if !strings.Contains(r.stdout, `"merge_request": "`+want+`"`) {
			t.Errorf("setting %s: merge request verdict is not %s\n%s", setting, want, r.stdout)
		}
	}
}

func TestLintErrors(t *testing.T) {
	clean := fixture("clean.gitlab-ci.yml")
	tests := []struct {
		name   string
		args   []string
		stderr string
	}{
		{"no file", []string{"lint"}, "accepts 1 arg(s), received 0"},
		{"missing file", []string{"lint", "nope.yml"}, "open nope.yml: no such file or directory"},
		{"bad format", []string{"lint", "--format", "xml", clean}, `--format must be text or json, not "xml"`},
		{"bad fail-on", []string{"lint", "--fail-on", "fatal", clean}, `--fail-on must be error, warning, info or none, not "fatal"`},
		{"bad setting", []string{"lint", "--skip-branch-pipelines-for-mrs", "maybe", clean},
			`--skip-branch-pipelines-for-mrs must be true, false or unknown, not "maybe"`},
		{"empty default branch", []string{"lint", "--default-branch", "", clean}, "--default-branch cannot be empty"},
		{"unknown flag", []string{"lint", "--colour", clean}, "unknown flag: --colour"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := pipewise(t, "", tt.args...)
			if r.code != exitError {
				t.Errorf("exit code = %d, want %d", r.code, exitError)
			}
			if r.stderr != "pipewise: "+tt.stderr+"\n" {
				t.Errorf("stderr = %q, want %q", r.stderr, "pipewise: "+tt.stderr+"\n")
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want nothing", r.stdout)
			}
		})
	}
}

type failingIO struct{}

func (failingIO) Read([]byte) (int, error)  { return 0, os.ErrClosed }
func (failingIO) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestLintIOErrors(t *testing.T) {
	var errb bytes.Buffer
	if code := run([]string{"lint", "-"}, failingIO{}, &bytes.Buffer{}, &errb); code != exitError ||
		!strings.Contains(errb.String(), "read standard input") {
		t.Errorf("unreadable stdin: exit %d, stderr %q", code, errb.String())
	}
	errb.Reset()
	if code := run([]string{"lint", fixture("clean.gitlab-ci.yml")}, strings.NewReader(""), failingIO{}, &errb); code != exitError ||
		!strings.Contains(errb.String(), "write the report") {
		t.Errorf("unwritable stdout: exit %d, stderr %q", code, errb.String())
	}
}

func TestFindingsErrorMessage(t *testing.T) {
	if got := (&findingsError{count: 2}).Error(); got != "2 findings at or above the --fail-on threshold" {
		t.Errorf("Error() = %q", got)
	}
}
