package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damienaulombard/pipewise/internal/eval"
	"github.com/damienaulombard/pipewise/internal/rules"
)

func explainFixture(name string) string {
	return filepath.Join("..", "..", "testdata", "explain", name)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// inProject copies the explain fixture to .gitlab-ci.yml in a temporary
// directory and moves there, as in a project checkout.
func inProject(t *testing.T) {
	t.Helper()
	data := readFile(t, explainFixture("explain.gitlab-ci.yml"))
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitlab-ci.yml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
}

func TestExplainOutput(t *testing.T) {
	want := map[string]string{
		"text": readFile(t, explainFixture("explain-test.txt")),
		"json": readFile(t, explainFixture("explain-test.json")),
	}
	inProject(t)
	tests := []struct {
		name   string
		stdin  string
		args   []string
		format string
	}{
		{"default file", "", []string{"explain", "test"}, "text"},
		{"named file", "", []string{"explain", "test", ".gitlab-ci.yml"}, "text"},
		{"json", "", []string{"explain", "--format", "json", "test"}, "json"},
		{"standard input", readFile(t, ".gitlab-ci.yml"), []string{"explain", "test", "-"}, "text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := pipewise(t, tt.stdin, tt.args...)
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			got := r.stdout
			if tt.stdin != "" {
				got = strings.Replace(got, "(<stdin>:", "(.gitlab-ci.yml:", 1)
			}
			if got != want[tt.format] {
				t.Errorf("output differs:\n--- got\n%s\n--- want\n%s", got, want[tt.format])
			}
		})
	}
}

func TestExplainOptions(t *testing.T) {
	noRules := writeConfig(t, "lint:\n  script: [x]\n")
	for setting, want := range map[string]string{"true": "runs", "false": "skipped", "unknown": "maybe"} {
		r := pipewise(t, "", "explain", "--skip-branch-pipelines-for-mrs", setting, "lint", noRules)
		if !strings.Contains(r.stdout, "merge request    "+want) {
			t.Errorf("setting %s: merge request verdict is not %s\n%s", setting, want, r.stdout)
		}
	}
	mainOnly := writeConfig(t, "unit:\n  script: [x]\n  rules:\n    - if: $CI_COMMIT_BRANCH == \"main\"\n")
	if r := pipewise(t, "", "explain", "--default-branch", "master", "unit", mainOnly); !strings.Contains(r.stdout, "default branch   skipped") {
		t.Errorf("default branch master:\n%s", r.stdout)
	}
}

func TestExplainErrors(t *testing.T) {
	fixture := explainFixture("explain.gitlab-ci.yml")
	orphan := writeConfig(t, "orphan:\n  extends: .missing\n  script: [x]\n")
	invalid := writeConfig(t, "a: [\n")
	tests := []struct {
		name   string
		args   []string
		stderr string
	}{
		{"no job", []string{"explain"}, "accepts between 1 and 2 arg(s), received 0"},
		{"too many arguments", []string{"explain", "test", fixture, "extra"}, "accepts between 1 and 2 arg(s), received 3"},
		{"unknown job", []string{"explain", "nope", fixture}, `no job "nope" in ` + fixture},
		{"hidden job", []string{"explain", ".template", fixture}, `no job ".template" in ` + fixture},
		{"job that is not analysed", []string{"explain", "orphan", orphan},
			`job "orphan" is not analysed: extends ".missing": no such job or hidden job`},
		{"invalid configuration", []string{"explain", "a", invalid},
			`no job "a" in ` + invalid + `: invalid YAML: yaml: line 1: did not find expected node content`},
		{"missing file", []string{"explain", "test", "nope.yml"}, "open nope.yml: no such file or directory"},
		{"bad format", []string{"explain", "--format", "xml", "test", fixture}, `--format must be text or json, not "xml"`},
		{"bad setting", []string{"explain", "--skip-branch-pipelines-for-mrs", "maybe", "test", fixture},
			`--skip-branch-pipelines-for-mrs must be true, false or unknown, not "maybe"`},
		{"empty default branch", []string{"explain", "--default-branch", "", "test", fixture}, "--default-branch cannot be empty"},
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

func TestExplainIOErrors(t *testing.T) {
	var errb bytes.Buffer
	if code := run([]string{"explain", "test", "-"}, failingIO{}, &bytes.Buffer{}, &errb); code != exitError ||
		!strings.Contains(errb.String(), "read standard input") {
		t.Errorf("unreadable stdin: exit %d, stderr %q", code, errb.String())
	}
	errb.Reset()
	if code := run([]string{"explain", "test", explainFixture("explain.gitlab-ci.yml")}, strings.NewReader(""), failingIO{}, &errb); code != exitError ||
		!strings.Contains(errb.String(), "write the explanation") {
		t.Errorf("unwritable stdout: exit %d, stderr %q", code, errb.String())
	}
}

// TestExplainExistingFixtures runs explain on every analysed job of the
// fixtures lint is tested with.
func TestExplainExistingFixtures(t *testing.T) {
	var files []string
	for _, dir := range []string{"config", "report"} {
		found, err := filepath.Glob(filepath.Join("..", "..", "testdata", dir, "*.gitlab-ci.yml"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, found...)
	}
	if len(files) < 7 {
		t.Fatalf("found %d fixtures: %v", len(files), files)
	}
	for _, file := range files {
		jobs := rules.Analyze([]byte(readFile(t, file)), eval.Options{DefaultBranch: "main"}).Jobs
		if len(jobs) == 0 {
			t.Errorf("%s: no job to explain", file)
		}
		for _, jr := range jobs {
			for _, format := range []string{"text", "json"} {
				r := pipewise(t, "", "explain", "--format", format, jr.Job.Name, file)
				if r.code != exitOK || r.stderr != "" || !strings.Contains(r.stdout, jr.Job.Name) {
					t.Errorf("%s, job %s, %s: exit %d, stderr %q\n%s", file, jr.Job.Name, format, r.code, r.stderr, r.stdout)
				}
			}
		}
	}
}
