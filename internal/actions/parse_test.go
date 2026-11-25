package actions

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/damienaulombard/pipewise/internal/config"
)

// root is the fixture repository: its workflows are under .github/workflows.
var root = filepath.Join("..", "..", "testdata", "actions")

func readRoot(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
}

func parseFixture(t *testing.T, name string) *Workflow {
	t.Helper()
	data, err := readRoot(".github/workflows/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return Parse(data, readRoot)
}

func TestParseSimple(t *testing.T) {
	w := parseFixture(t, "simple.yml")
	if len(w.Problems) != 0 {
		t.Fatalf("problems: %+v", w.Problems)
	}
	if w.Name != "CI" {
		t.Errorf("name = %q", w.Name)
	}
	wantTriggers := []Trigger{
		{Event: "pull_request", Line: 4},
		{Event: "push", Line: 5, Branches: []string{"main"}, Tags: []string{"v*"}, Paths: true},
	}
	if !reflect.DeepEqual(w.Triggers, wantTriggers) {
		t.Errorf("triggers = %+v\nwant %+v", w.Triggers, wantTriggers)
	}
	wantJobs := []*Job{
		{Name: "test", Line: 11, Steps: []Step{
			{Line: 14, Uses: "actions/checkout@v4"},
			{Line: 15, Name: "Test", Run: true},
		}},
		{Name: "release", Line: 17, Needs: []string{"test"}, If: "startsWith(github.ref, 'refs/tags/')", Steps: []Step{
			{Line: 22, Uses: "actions/checkout@v4"},
			{Line: 23, Run: true},
		}},
	}
	if !reflect.DeepEqual(w.Jobs, wantJobs) {
		t.Errorf("jobs:\n%s\nwant\n%s", dump(w.Jobs), dump(wantJobs))
	}
}

func TestParseLocalReusable(t *testing.T) {
	w := parseFixture(t, "caller.yml")
	if len(w.Problems) != 0 {
		t.Fatalf("problems: %+v", w.Problems)
	}
	if got := events(w.Triggers); !reflect.DeepEqual(got, []string{"push", "pull_request"}) {
		t.Errorf("events = %v", got)
	}
	if len(w.Jobs) != 2 {
		t.Fatalf("jobs: %s", dump(w.Jobs))
	}
	for _, j := range w.Jobs {
		c := j.Uses
		if c == nil || c.Ref != "./.github/workflows/reusable.yml" || !c.Local || c.Workflow == nil {
			t.Fatalf("job %s: call = %+v", j.Name, c)
		}
		if got := names(c.Workflow.Jobs); !reflect.DeepEqual(got, []string{"compile"}) {
			t.Errorf("job %s: called jobs = %v", j.Name, got)
		}
		if got := events(c.Workflow.Triggers); !reflect.DeepEqual(got, []string{"workflow_call"}) {
			t.Errorf("job %s: called events = %v", j.Name, got)
		}
	}
	if w.Jobs[1].Uses.Line != 8 || !reflect.DeepEqual(w.Jobs[1].Needs, []string{"build"}) {
		t.Errorf("deploy = %+v", w.Jobs[1])
	}
}

func TestParseRemoteReusable(t *testing.T) {
	w := parseFixture(t, "remote.yml")
	want := []config.Problem{{
		Kind: KindUnresolvedWorkflow, Job: "shared", Line: 5,
		Message: `uses: acme/ci/.github/workflows/build.yml@v2 is a workflow in another repository; pipewise does not fetch it, so its jobs are not analysed`,
	}}
	if !reflect.DeepEqual(w.Problems, want) {
		t.Errorf("problems = %+v\nwant %+v", w.Problems, want)
	}
	if got := names(w.Jobs); !reflect.DeepEqual(got, []string{"shared", "local"}) {
		t.Errorf("jobs = %v", got)
	}
	if c := w.Jobs[0].Uses; c == nil || c.Local || c.Workflow != nil {
		t.Errorf("call = %+v", c)
	}
}

func TestParseInvalidFixture(t *testing.T) {
	w := parseFixture(t, "invalid.yml")
	want := []config.Problem{
		{Kind: config.KindParseError, Job: "no-runner", Line: 4, Message: `job "no-runner" needs runs-on, or uses: for a reusable workflow`},
		{Kind: config.KindParseError, Job: "bad-step", Line: 10, Message: `step 1 of job "bad-step" needs uses: or run:`},
		{Kind: config.KindParseError, Job: "good", Line: 12, Message: `job "good" needs "missing", which is not a job of this workflow`},
	}
	if !reflect.DeepEqual(w.Problems, want) {
		t.Errorf("problems:\n%+v\nwant\n%+v", w.Problems, want)
	}
	if got := names(w.Jobs); !reflect.DeepEqual(got, []string{"no-runner", "bad-step", "good"}) {
		t.Errorf("jobs = %v", got)
	}
}

func TestParseProblems(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []config.Problem
	}{
		{"invalid YAML", "on: [push\n", []config.Problem{{Kind: config.KindParseError,
			Message: "invalid YAML: yaml: line 1: did not find expected ',' or ']'"}}},
		{"empty file", "", []config.Problem{{Kind: config.KindParseError, Message: "the workflow is empty"}}},
		{"not a mapping", "- push\n", []config.Problem{{Kind: config.KindParseError, Line: 1,
			Message: "the workflow must be a mapping of keywords"}}},
		{"no on", "jobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n", []config.Problem{{Kind: config.KindParseError,
			Message: "the workflow has no on: keyword, so nothing starts it"}}},
		{"on not an event", "on: {push: [main]}\njobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n",
			[]config.Problem{{Kind: config.KindParseError, Line: 1, Message: `on: push must be empty or a mapping of filters`}}},
		{"on list of mappings", "on: [{push: null}]\njobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n",
			[]config.Problem{{Kind: config.KindParseError, Line: 1, Message: `on: must be an event, a list of events or a mapping`}}},
		{"on empty", "on: []\njobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n",
			[]config.Problem{{Kind: config.KindParseError, Line: 1, Message: `on: names no event`}}},
		{"bad filter", "on:\n  push:\n    branches: {a: b}\njobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n",
			[]config.Problem{{Kind: config.KindParseError, Line: 3, Message: `on: push: branches must be a pattern or a list of patterns`}}},
		{"filter list with a mapping", "on:\n  push:\n    tags: [{a: b}]\njobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n",
			[]config.Problem{{Kind: config.KindParseError, Line: 3, Message: `on: push: tags must be a pattern or a list of patterns`}}},
		{"types not a list", "on:\n  pull_request:\n    types: {a: b}\njobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n",
			[]config.Problem{{Kind: config.KindParseError, Line: 3, Message: `on: pull_request: types must be an activity type or a list of activity types`}}},
		{"schedule takes a list", "on:\n  schedule:\n    - cron: '0 0 * * *'\n  workflow_dispatch:\n    inputs: {}\njobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n", nil},
		{"no jobs", "on: push\n", []config.Problem{{Kind: config.KindParseError,
			Message: "the workflow has no jobs: keyword"}}},
		{"jobs not a mapping", "on: push\njobs: [a]\n", []config.Problem{{Kind: config.KindParseError, Line: 2,
			Message: "jobs: must be a mapping of job ids to jobs"}}},
		{"jobs empty", "on: push\njobs: {}\n", []config.Problem{{Kind: config.KindParseError, Line: 2,
			Message: "jobs: must define at least one job"}}},
		{"job not a mapping", "on: push\njobs:\n  a: run\n", []config.Problem{{Kind: config.KindParseError, Job: "a", Line: 3,
			Message: `job "a" must be a mapping`}}},
		{"steps and uses", "on: push\njobs:\n  a:\n    uses: ./.github/workflows/reusable.yml\n    steps: [{run: a}]\n",
			[]config.Problem{{Kind: config.KindParseError, Job: "a", Line: 3,
				Message: `job "a" calls a reusable workflow, so it cannot have steps or runs-on`}}},
		{"no steps", "on: push\njobs:\n  a:\n    runs-on: x\n", []config.Problem{{Kind: config.KindParseError, Job: "a", Line: 3,
			Message: `job "a" needs steps:`}}},
		{"steps not a list", "on: push\njobs:\n  a:\n    runs-on: x\n    steps: run\n", []config.Problem{{Kind: config.KindParseError, Job: "a", Line: 5,
			Message: `steps: of job "a" must be a list`}}},
		{"step not a mapping", "on: push\njobs:\n  a:\n    runs-on: x\n    steps: [echo]\n", []config.Problem{{Kind: config.KindParseError, Job: "a", Line: 5,
			Message: `step 1 of job "a" must be a mapping`}}},
		{"step with uses and run", "on: push\njobs:\n  a:\n    runs-on: x\n    steps: [{uses: a/b@v1, run: c}]\n",
			[]config.Problem{{Kind: config.KindParseError, Job: "a", Line: 5, Message: `step 1 of job "a" has both uses: and run:`}}},
		{"step uses not a string", "on: push\njobs:\n  a:\n    runs-on: x\n    steps: [{uses: [a]}]\n",
			[]config.Problem{{Kind: config.KindParseError, Job: "a", Line: 5, Message: `step 1 of job "a": uses: must be a string`}}},
		{"needs not a list", "on: push\njobs:\n  a:\n    runs-on: x\n    needs: {b: c}\n    steps: [{run: a}]\n",
			[]config.Problem{{Kind: config.KindParseError, Job: "a", Line: 5, Message: `needs: of job "a" must be a job id or a list of job ids`}}},
		{"if not a scalar", "on: push\njobs:\n  a:\n    runs-on: x\n    if: [a]\n    steps: [{run: a}]\n",
			[]config.Problem{{Kind: config.KindParseError, Job: "a", Line: 5, Message: `if: of job "a" must be an expression`}}},
		{"uses not a string", "on: push\njobs:\n  a:\n    uses: [a]\n",
			[]config.Problem{{Kind: config.KindParseError, Job: "a", Line: 4, Message: `uses: of job "a" must be a string`}}},
		{"local with ref", "on: push\njobs:\n  a:\n    uses: ./.github/workflows/reusable.yml@v1\n",
			[]config.Problem{{Kind: config.KindParseError, Job: "a", Line: 4,
				Message: `uses: ./.github/workflows/reusable.yml@v1: a workflow in the same repository takes no @ref`}}},
		{"remote without ref", "on: push\njobs:\n  a:\n    uses: acme/ci/.github/workflows/b.yml\n",
			[]config.Problem{{Kind: config.KindParseError, Job: "a", Line: 4,
				Message: `uses: acme/ci/.github/workflows/b.yml: a workflow in another repository needs @ref`}}},
		{"outside the repository", "on: push\njobs:\n  a:\n    uses: ./../other/.github/workflows/b.yml\n",
			[]config.Problem{{Kind: KindUnresolvedWorkflow, Job: "a", Line: 4,
				Message: `uses: ./../other/.github/workflows/b.yml points outside the repository; its jobs are not analysed`}}},
		{"missing local", "on: push\njobs:\n  a:\n    uses: ./.github/workflows/missing.yml\n",
			[]config.Problem{{Kind: KindUnresolvedWorkflow, Job: "a", Line: 4,
				Message: `uses: ./.github/workflows/missing.yml: no such file in the repository; its jobs are not analysed`}}},
		{"aliases are followed", "on: &ev push\njobs:\n  a:\n    runs-on: x\n    steps: &s [{run: a}]\n  b:\n    runs-on: x\n    steps: *s\n", nil},
		{"null if and needs", "on: push\njobs:\n  a:\n    runs-on: x\n    if:\n    needs:\n    steps: [{run: a}]\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := Parse([]byte(tt.yaml), readRoot)
			if !reflect.DeepEqual(w.Problems, tt.want) {
				t.Errorf("problems:\n%+v\nwant\n%+v", w.Problems, tt.want)
			}
		})
	}
}

func TestParseTypes(t *testing.T) {
	tests := []struct {
		name string
		on   string
		want []string
	}{
		{"absent", "on:\n  pull_request:\n    branches: [main]\n", nil},
		{"list", "on:\n  pull_request:\n    types: [opened, synchronize]\n", []string{"opened", "synchronize"}},
		{"single string", "on:\n  pull_request:\n    types: labeled\n", []string{"labeled"}},
		// An empty list sets no activity type: GitHub's defaults apply, as
		// when types: is absent.
		{"empty list", "on:\n  pull_request:\n    types: []\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := Parse([]byte(tt.on+"jobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n"), readRoot)
			if len(w.Problems) != 0 || len(w.Triggers) != 1 {
				t.Fatalf("problems %+v, triggers %+v", w.Problems, w.Triggers)
			}
			if !reflect.DeepEqual(w.Triggers[0].Types, tt.want) {
				t.Errorf("types = %q, want %q", w.Triggers[0].Types, tt.want)
			}
		})
	}
}

func TestParseCalledWorkflowProblems(t *testing.T) {
	files := map[string]string{
		".github/workflows/broken.yml": "on: workflow_call\njobs:\n  x:\n    runs-on: y\n",
		".github/workflows/loop.yml":   "on: workflow_call\njobs:\n  again:\n    uses: ./.github/workflows/loop.yml\n",
		".github/workflows/ping.yml":   "on: workflow_call\njobs:\n  p:\n    uses: ./.github/workflows/pong.yml\n",
		".github/workflows/pong.yml":   "on: workflow_call\njobs:\n  q:\n    uses: ./.github/workflows/ping.yml\n",
	}
	read := func(path string) ([]byte, error) {
		if s, ok := files[path]; ok {
			return []byte(s), nil
		}
		return nil, fs.ErrNotExist
	}
	tests := []struct {
		name string
		yaml string
		want []config.Problem
	}{
		{"problem in the called file", "on: push\njobs:\n  a:\n    uses: ./.github/workflows/broken.yml\n",
			[]config.Problem{{Kind: config.KindParseError, Job: "a", Line: 4,
				Message: `in .github/workflows/broken.yml:3: job "x" needs steps:`}}},
		{"cycle", "on: push\njobs:\n  a:\n    uses: ./.github/workflows/loop.yml\n",
			[]config.Problem{{Kind: KindUnresolvedWorkflow, Job: "a", Line: 4,
				Message: `in .github/workflows/loop.yml:4: uses: ./.github/workflows/loop.yml calls itself, directly or through another workflow`}}},
		{"cycle through two files", "on: push\njobs:\n  a:\n    uses: ./.github/workflows/ping.yml\n",
			[]config.Problem{{Kind: KindUnresolvedWorkflow, Job: "a", Line: 4,
				Message: `in .github/workflows/pong.yml:4: uses: ./.github/workflows/ping.yml calls itself, directly or through another workflow`}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := Parse([]byte(tt.yaml), read)
			if !reflect.DeepEqual(w.Problems, tt.want) {
				t.Errorf("problems:\n%+v\nwant\n%+v", w.Problems, tt.want)
			}
		})
	}
}

// TestParseCallsAreBounded checks that a tree of calls costs one read per
// file and stops at the nesting GitHub allows: each workflow calls the next
// one twice, which would be read 2^n times otherwise.
func TestParseCallsAreBounded(t *testing.T) {
	const n = 40
	files := map[string]string{}
	for i := range n {
		next := fmt.Sprintf("./.github/workflows/w%d.yml", i+1)
		files[fmt.Sprintf(".github/workflows/w%d.yml", i)] = fmt.Sprintf(
			"on: workflow_call\njobs:\n  a:\n    uses: %s\n  b:\n    uses: %s\n", next, next)
	}
	reads := map[string]int{}
	read := func(path string) ([]byte, error) {
		reads[path]++
		if s, ok := files[path]; ok {
			return []byte(s), nil
		}
		return nil, fs.ErrNotExist
	}
	w := Parse([]byte("on: push\njobs:\n  top:\n    uses: ./.github/workflows/w0.yml\n"), read)
	for path, count := range reads {
		if count != 1 {
			t.Errorf("%s read %d times", path, count)
		}
	}
	if len(reads) != 9 {
		t.Errorf("read %d files, want the 9 levels GitHub allows below the caller: %v", len(reads), reads)
	}
	want := []config.Problem{
		{Kind: config.KindParseError, Job: "top", Line: 4,
			Message: "in .github/workflows/w8.yml:4: uses: ./.github/workflows/w9.yml: GitHub allows at most ten levels of workflows, the caller included"},
		{Kind: config.KindParseError, Job: "top", Line: 4,
			Message: "in .github/workflows/w8.yml:6: uses: ./.github/workflows/w9.yml: GitHub allows at most ten levels of workflows, the caller included"},
	}
	if !reflect.DeepEqual(w.Problems, want) {
		t.Errorf("problems:\n%+v\nwant\n%+v", w.Problems, want)
	}
}

func TestParseReportsACalledWorkflowOnce(t *testing.T) {
	read := func(string) ([]byte, error) {
		return []byte("on: workflow_call\njobs:\n  x:\n    runs-on: y\n"), nil
	}
	w := Parse([]byte("on: push\njobs:\n  a:\n    uses: ./.github/workflows/broken.yml\n  b:\n    uses: ./.github/workflows/broken.yml\n"), read)
	want := []config.Problem{{Kind: config.KindParseError, Job: "a", Line: 4,
		Message: `in .github/workflows/broken.yml:3: job "x" needs steps:`}}
	if !reflect.DeepEqual(w.Problems, want) {
		t.Errorf("problems:\n%+v\nwant\n%+v", w.Problems, want)
	}
	if w.Jobs[0].Uses.Workflow == nil || w.Jobs[0].Uses.Workflow != w.Jobs[1].Uses.Workflow {
		t.Errorf("calls = %+v, %+v; want the same parsed workflow", w.Jobs[0].Uses, w.Jobs[1].Uses)
	}
}

func TestParseWithoutReader(t *testing.T) {
	w := Parse([]byte("on: push\njobs:\n  a:\n    uses: ./.github/workflows/reusable.yml\n"), nil)
	want := []config.Problem{{Kind: KindUnresolvedWorkflow, Job: "a", Line: 4,
		Message: `uses: ./.github/workflows/reusable.yml: the repository is unknown (standard input, or a file outside .github/workflows); its jobs are not analysed`}}
	if !reflect.DeepEqual(w.Problems, want) {
		t.Errorf("problems:\n%+v\nwant\n%+v", w.Problems, want)
	}
}

func TestParseReadError(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, errors.New("permission denied") }
	w := Parse([]byte("on: push\njobs:\n  a:\n    uses: ./.github/workflows/reusable.yml\n"), read)
	want := []config.Problem{{Kind: KindUnresolvedWorkflow, Job: "a", Line: 4,
		Message: `uses: ./.github/workflows/reusable.yml: cannot read it (permission denied); its jobs are not analysed`}}
	if !reflect.DeepEqual(w.Problems, want) {
		t.Errorf("problems:\n%+v\nwant\n%+v", w.Problems, want)
	}
}

func TestRepositoryRoot(t *testing.T) {
	tests := []struct {
		path, root string
		ok         bool
	}{
		{".github/workflows/ci.yml", ".", true},
		{"repo/.github/workflows/ci.yml", "repo", true},
		{"/abs/repo/.github/workflows/ci.yaml", "/abs/repo", true},
		{".gitlab-ci.yml", "", false},
		{"ci/.github/ci.yml", "", false},
		{"-", "", false},
	}
	for _, tt := range tests {
		root, ok := RepositoryRoot(filepath.FromSlash(tt.path))
		if ok != tt.ok || root != filepath.FromSlash(tt.root) {
			t.Errorf("RepositoryRoot(%q) = %q, %v; want %q, %v", tt.path, root, ok, tt.root, tt.ok)
		}
	}
}

func TestDirReader(t *testing.T) {
	read := DirReader(root)
	if _, err := read(".github/workflows/simple.yml"); err != nil {
		t.Errorf("read: %v", err)
	}
	if _, err := read(".github/workflows/missing.yml"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: %v", err)
	}
	if _, err := DirReader(filepath.Join(root, "missing"))("x.yml"); err == nil {
		t.Error("missing root: no error")
	}
	if _, err := read("../config/simple.gitlab-ci.yml"); err == nil {
		t.Error("read outside the root: no error")
	}
}

func events(ts []Trigger) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Event)
	}
	return out
}

func names(js []*Job) []string {
	var out []string
	for _, j := range js {
		out = append(out, j.Name)
	}
	return out
}

func dump(js []*Job) string {
	var b strings.Builder
	for _, j := range js {
		fmt.Fprintf(&b, "%+v\n", *j)
	}
	return b.String()
}
