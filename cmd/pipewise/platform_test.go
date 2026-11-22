package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workflow(name string) string {
	return filepath.Join("..", "..", "testdata", "actions", ".github", "workflows", name)
}

func TestDetectPlatform(t *testing.T) {
	tests := []struct {
		flag, path, want string
	}{
		{"auto", ".github/workflows/ci.yml", platformGitHub},
		{"auto", "repo/.github/workflows/ci.yaml", platformGitHub},
		{"auto", ".gitlab-ci.yml", platformGitLab},
		{"auto", "-", platformGitLab},
		{"auto", "ci/workflows/ci.yml", platformGitLab},
		{"gitlab", ".github/workflows/ci.yml", platformGitLab},
		{"github", "-", platformGitHub},
		{"github", "ci.yml", platformGitHub},
	}
	for _, tt := range tests {
		got, err := detectPlatform(tt.flag, filepath.FromSlash(tt.path))
		if err != nil || got != tt.want {
			t.Errorf("detectPlatform(%q, %q) = %q, %v; want %q", tt.flag, tt.path, got, err, tt.want)
		}
	}
	if _, err := detectPlatform("circleci", "x"); err == nil ||
		err.Error() != `--platform must be auto, gitlab or github, not "circleci"` {
		t.Errorf("bad flag: %v", err)
	}
}

func TestLintWorkflow(t *testing.T) {
	remote, caller, invalid := workflow("remote.yml"), workflow("caller.yml"), workflow("invalid.yml")
	tests := []struct {
		name     string
		stdin    string
		args     []string
		code     int
		contains []string
		absent   []string
	}{
		{"simple workflow", "", []string{"lint", workflow("simple.yml")}, exitOK,
			[]string{"info not-evaluated", ": 1 finding: 0 errors, 0 warnings, 1 info."}, nil},
		{"local reusable workflow is read", "", []string{"lint", caller}, exitOK,
			[]string{"info not-evaluated"}, []string{"unresolved-workflow"}},
		{"remote reusable workflow", "", []string{"lint", remote}, exitOK,
			[]string{remote + ":5: warning unresolved-workflow", "acme/ci/.github/workflows/build.yml@v2"}, nil},
		{"remote reusable workflow fails --fail-on warning", "", []string{"lint", "--fail-on", "warning", remote}, exitFindings,
			[]string{"unresolved-workflow"}, nil},
		{"invalid workflow", "", []string{"lint", invalid}, exitFindings,
			[]string{invalid + ":4: error parse-error", "GitHub would reject"}, nil},
		{"json report", "", []string{"lint", "--format", "json", remote}, exitOK,
			[]string{`"schema": 1`, `"rule": "not-evaluated"`, `"rule": "unresolved-workflow"`, `"jobs": []`}, []string{"platform"}},
		{"stdin with --platform github", "on: push\njobs:\n  a:\n    uses: ./.github/workflows/reusable.yml\n",
			[]string{"lint", "--platform", "github", "-"}, exitOK,
			[]string{"<stdin>:4: warning unresolved-workflow", "the repository is unknown"}, nil},
		{"stdin is GitLab by default", "on: push\n", []string{"lint", "-"}, exitFindings,
			[]string{`job "on" must be a mapping`, "GitLab would reject"}, []string{"not-evaluated"}},
		{"--platform gitlab on a workflow file", "", []string{"lint", "--platform", "gitlab", "--fail-on", "none", caller}, exitOK,
			nil, []string{"not-evaluated"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := pipewise(t, tt.stdin, tt.args...)
			if r.code != tt.code || r.stderr != "" {
				t.Errorf("exit %d, want %d; stderr %q\n%s", r.code, tt.code, r.stderr, r.stdout)
			}
			for _, s := range tt.contains {
				if !strings.Contains(r.stdout, s) {
					t.Errorf("report lacks %q:\n%s", s, r.stdout)
				}
			}
			for _, s := range tt.absent {
				if strings.Contains(r.stdout, s) {
					t.Errorf("report has %q:\n%s", s, r.stdout)
				}
			}
		})
	}
}

func TestLintWorkflowOutsideWorkflowsDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ci.yml")
	if err := os.WriteFile(path, []byte("on: push\njobs:\n  a:\n    uses: ./.github/workflows/x.yml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := pipewise(t, "", "lint", "--platform", "github", path)
	if r.code != exitOK || !strings.Contains(r.stdout, "the repository is unknown") {
		t.Errorf("exit %d\n%s", r.code, r.stdout)
	}
}

func TestPlatformErrors(t *testing.T) {
	for _, args := range [][]string{
		{"lint", "--platform", "circleci", workflow("simple.yml")},
		{"explain", "--platform", "circleci", "test", workflow("simple.yml")},
	} {
		r := pipewise(t, "", args...)
		if r.code != exitError || r.stderr != "pipewise: --platform must be auto, gitlab or github, not \"circleci\"\n" || r.stdout != "" {
			t.Errorf("%v: exit %d, stderr %q, stdout %q", args, r.code, r.stderr, r.stdout)
		}
	}
}

func TestExplainWorkflow(t *testing.T) {
	simple, invalid := workflow("simple.yml"), workflow("invalid.yml")
	tests := []struct {
		name   string
		stdin  string
		args   []string
		stderr string
	}{
		{"existing job", "", []string{"explain", "test", simple},
			`job "test" is not analysed: GitHub Actions workflows are not evaluated yet`},
		{"job with a problem", "", []string{"explain", "no-runner", invalid},
			`job "no-runner" is not analysed: GitHub Actions workflows are not evaluated yet`},
		{"unknown job", "", []string{"explain", "nope", simple}, `no job "nope" in ` + simple},
		{"invalid workflow", "", []string{"explain", "a", "--platform", "github", "-"},
			`no job "a" in <stdin>: the workflow is empty`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := pipewise(t, tt.stdin, tt.args...)
			if r.code != exitError || r.stderr != "pipewise: "+tt.stderr+"\n" || r.stdout != "" {
				t.Errorf("exit %d, stderr %q, want %q; stdout %q", r.code, r.stderr, tt.stderr, r.stdout)
			}
		})
	}
}
