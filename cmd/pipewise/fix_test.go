package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fixable = `unit:
  script: [make test]
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
  artifacts:
    paths: [bin/]
`

func TestLintFix(t *testing.T) {
	path := writeConfig(t, fixable)
	r := pipewise(t, "", "lint", "--fix", path)
	if r.code != exitOK {
		t.Errorf("exit code = %d, want %d (info findings, as without --fix)\nstderr: %s", r.code, exitOK, r.stderr)
	}
	name := filepath.ToSlash(path)
	want := "--- a/" + name + "\n+++ b/" + name + "\n" + `@@ -1,6 +1,8 @@
 unit:
+  interruptible: true
   script: [make test]
   rules:
     - if: $CI_PIPELINE_SOURCE == "merge_request_event"
   artifacts:
+    expire_in: 1 week
     paths: [bin/]
`
	if r.stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", r.stdout, want)
	}
	if r.stderr != "" {
		t.Errorf("stderr = %q", r.stderr)
	}
	if data, _ := os.ReadFile(path); string(data) != fixable {
		t.Errorf("the file was modified:\n%s", data)
	}

	// The exit code is the one lint gives without --fix.
	if r := pipewise(t, "", "lint", "--fix", "--fail-on", "info", path); r.code != exitFindings {
		t.Errorf("--fail-on info: exit %d, want %d", r.code, exitFindings)
	}
	r = pipewise(t, "", "lint", "--fix", "--fix-expire-in", "3 days", path)
	if !strings.Contains(r.stdout, "+    expire_in: 3 days\n") {
		t.Errorf("--fix-expire-in:\n%s", r.stdout)
	}
	if r := pipewise(t, "", "lint", "--fix", fixture("clean.gitlab-ci.yml")); r.code != exitOK || r.stdout != "" {
		t.Errorf("nothing to fix: %+v", r)
	}
}

// The diff applies with git apply, from the directory pipewise ran in, and
// lint no longer reports what it fixed.
func TestLintFixApplies(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitlab-ci.yml"), []byte(fixable), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	r := pipewise(t, "", "lint", "--fix", ".gitlab-ci.yml")
	cmd := exec.CommandContext(t.Context(), gitBin, "apply", "-")
	cmd.Stdin = strings.NewReader(r.stdout)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git apply: %v\n%s\ndiff:\n%s", err, out, r.stdout)
	}
	after := pipewise(t, "", "lint", "--fail-on", "none", ".gitlab-ci.yml")
	if strings.Contains(after.stdout, "missing-interruptible") || strings.Contains(after.stdout, "artifacts-no-expiry") {
		t.Errorf("still reported after git apply:\n%s", after.stdout)
	}
}

func TestLintFixSkipped(t *testing.T) {
	path := writeConfig(t, `.mr: &mr
  script: [make test]
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
alias: *mr
`)
	r := pipewise(t, "", "lint", "--fix", path)
	want := "pipewise: --fix skipped missing-interruptible on job \"alias\" (line 5): " +
		"\"alias\" is an alias of another mapping, shared with whatever else uses that anchor\n"
	if r.code != exitOK || r.stdout != "" || r.stderr != want {
		t.Errorf("got %+v\nwant stderr %q", r, want)
	}

	path = writeConfig(t, "default:\n  artifacts: {paths: [x]}\nbuild:\n  script: [x]\n")
	r = pipewise(t, "", "lint", "--fix", path)
	if !strings.Contains(r.stderr, "--fix skipped artifacts-no-expiry on default: (line 1)") {
		t.Errorf("stderr = %q", r.stderr)
	}
}

func TestLintFixErrors(t *testing.T) {
	path := writeConfig(t, fixable)
	workflow := filepath.Join(t.TempDir(), "ci.yml")
	if err := os.WriteFile(workflow, []byte("on: push\njobs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		args   []string
		stderr string
	}{
		{"stdin", []string{"lint", "--fix", "-"}, "--fix needs a file: the diff names the file it applies to"},
		{"expire-in without fix", []string{"lint", "--fix-expire-in", "1 day", path}, "--fix-expire-in only applies with --fix"},
		{"empty expire-in", []string{"lint", "--fix", "--fix-expire-in", "", path}, "--fix-expire-in cannot be empty"},
		{"multi-line expire-in", []string{"lint", "--fix", "--fix-expire-in", "1\nday", path}, "--fix-expire-in must be on one line"},
		{"GitHub Actions", []string{"lint", "--fix", "--platform", "github", workflow}, "--fix applies to GitLab CI configurations only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := pipewise(t, "", tt.args...)
			if r.code != exitError || r.stdout != "" || r.stderr != "pipewise: "+tt.stderr+"\n" {
				t.Errorf("got %+v\nwant stderr %q", r, tt.stderr)
			}
		})
	}
}

func TestLintFixWriteError(t *testing.T) {
	var errb bytes.Buffer
	path := writeConfig(t, fixable)
	if code := run([]string{"lint", "--fix", path}, strings.NewReader(""), failingIO{}, &errb); code != exitError ||
		!strings.Contains(errb.String(), "write the diff") {
		t.Errorf("unwritable stdout: exit %d, stderr %q", code, errb.String())
	}
}

const redundantConfig = `build:
  script: [make build]
  interruptible: true
`

func TestLintFixRedundant(t *testing.T) {
	path := writeConfig(t, redundantConfig)
	r := pipewise(t, "", "lint", "--fix", "--fail-on", "none",
		"--merged-results-pipelines", "true", "--protected-default-branch", "true", path)
	name := filepath.ToSlash(path)
	want := "--- a/" + name + "\n+++ b/" + name + "\n" + `@@ -1,3 +1,5 @@
 build:
+  rules:
+    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
   script: [make build]
   interruptible: true
`
	if r.code != exitOK || r.stdout != want || r.stderr != "" {
		t.Errorf("got %+v\nwant stdout\n%s", r, want)
	}

	// Without both preconditions, the job is named on standard error.
	r = pipewise(t, "", "lint", "--fix", "--fail-on", "none", "--merged-results-pipelines", "true", path)
	wantErr := "pipewise: --fix skipped redundant-job on job \"build\" (line 1): " +
		"direct pushes to the default branch are not known to be refused (--protected-default-branch=unknown)"
	if r.code != exitOK || r.stdout != "" || !strings.HasPrefix(r.stderr, wantErr) ||
		!strings.Contains(r.stderr, "Settings > Repository > Protected branches") {
		t.Errorf("got %+v\nwant stderr starting with %q", r, wantErr)
	}

	// With --fix, the exit code is still the one of the findings.
	r = pipewise(t, "", "lint", "--fix", "--merged-results-pipelines", "false", "--protected-default-branch", "true", path)
	if r.code != exitFindings || r.stdout != "" ||
		!strings.Contains(r.stderr, "merged results pipelines are not enabled (--merged-results-pipelines=false)") {
		t.Errorf("got %+v", r)
	}
}

func TestLintFixRedundantFlags(t *testing.T) {
	path := writeConfig(t, redundantConfig)
	tests := []struct {
		name   string
		args   []string
		stderr string
	}{
		{"merged results without fix", []string{"lint", "--merged-results-pipelines", "true", path},
			"--merged-results-pipelines only applies with --fix"},
		{"protected without fix", []string{"lint", "--protected-default-branch", "true", path},
			"--protected-default-branch only applies with --fix"},
		{"merged results value", []string{"lint", "--fix", "--merged-results-pipelines", "yes", path},
			`--merged-results-pipelines must be true, false or unknown, not "yes"`},
		{"protected value", []string{"lint", "--fix", "--protected-default-branch", "", path},
			`--protected-default-branch must be true, false or unknown, not ""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := pipewise(t, "", tt.args...)
			if r.code != exitError || r.stdout != "" || r.stderr != "pipewise: "+tt.stderr+"\n" {
				t.Errorf("got %+v\nwant stderr %q", r, tt.stderr)
			}
		})
	}
}
