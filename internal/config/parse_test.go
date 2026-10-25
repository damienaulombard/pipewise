package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func ptr[T any](v T) *T { return &v }

func parseFixture(t *testing.T, name string) *Pipeline {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "config", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return Parse(data)
}

func assertJobs(t *testing.T, got []*Job, want []*Job) {
	t.Helper()
	if len(got) != len(want) {
		names := make([]string, len(got))
		for i, j := range got {
			names[i] = j.Name
		}
		t.Fatalf("got %d jobs %v, want %d", len(got), names, len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("job %d:\n got  %+v\n want %+v", i, *got[i], *want[i])
		}
	}
}

func TestParseSimple(t *testing.T) {
	p := parseFixture(t, "simple.gitlab-ci.yml")

	if len(p.Problems) != 0 {
		t.Fatalf("unexpected problems: %+v", p.Problems)
	}
	wantWorkflow := &Workflow{Rules: []Rule{
		{If: `$CI_PIPELINE_SOURCE == "merge_request_event"`},
		{If: `$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH`},
		{If: `$CI_COMMIT_TAG`},
	}}
	if !reflect.DeepEqual(p.Workflow, wantWorkflow) {
		t.Errorf("workflow:\n got  %+v\n want %+v", p.Workflow, wantWorkflow)
	}
	assertJobs(t, p.Jobs, []*Job{
		{
			Name: "build", Line: 9, Stage: "build",
			Image:         &Image{Name: "golang:1.26"},
			Artifacts:     &Artifacts{ExpireIn: "1 week"},
			Interruptible: ptr(true),
		},
		{
			Name: "unit", Line: 18, Stage: "test",
			Image:    &Image{Name: "golang:1.26@sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			HasRules: true,
			Rules: []Rule{
				{If: `$CI_PIPELINE_SOURCE == "merge_request_event"`, Changes: true},
				{If: `$CI_COMMIT_TAG`, When: "never"},
				{Exists: true, When: "manual", AllowFailure: ptr(true)},
			},
		},
		{Name: "lint", Line: 32, Stage: "test"},
		{
			Name: "deploy", Line: 36, Stage: "deploy", When: "manual",
			Only:   &Filter{Refs: []string{"tags"}, Variables: []string{"$DEPLOY_ENABLED"}},
			Except: &Filter{Refs: []string{"branches"}},
		},
		{Name: "downstream", Line: 45, Stage: "deploy", Trigger: true},
	})
}

func TestParseNestedExtends(t *testing.T) {
	p := parseFixture(t, "nested-extends.gitlab-ci.yml")

	if len(p.Problems) != 0 {
		t.Fatalf("unexpected problems: %+v", p.Problems)
	}
	if p.Workflow != nil {
		t.Errorf("workflow = %+v, want nil", p.Workflow)
	}
	assertJobs(t, p.Jobs, []*Job{
		{
			// Three levels; the job's own rules replace the inherited ones, the
			// image of .go beats default:, interruptible comes from default:.
			Name: "unit", Line: 27, Stage: "test",
			HasRules:      true,
			Rules:         []Rule{{If: "$CI_COMMIT_BRANCH"}},
			Image:         &Image{Name: "golang:1.26"},
			Artifacts:     &Artifacts{ExpireIn: "1 day"},
			Interruptible: ptr(true),
			Extends:       []string{".go"},
		},
		{
			// The last parent's rules win; artifacts from both are merged.
			Name: "package", Line: 34, Stage: "test",
			HasRules:      true,
			Rules:         []Rule{{If: "$CI_COMMIT_TAG"}},
			Image:         &Image{Name: "golang:1.26"},
			Artifacts:     &Artifacts{ExpireIn: "1 day"},
			Interruptible: ptr(true),
			Extends:       []string{".go", ".release"},
		},
		{Name: "standalone", Line: 39, Stage: "test"},
		{Name: "partial", Line: 45, Stage: "test", Image: &Image{Name: "alpine:3.20"}},
	})
}

func TestParseAnchors(t *testing.T) {
	p := parseFixture(t, "anchors.gitlab-ci.yml")

	if len(p.Problems) != 0 {
		t.Fatalf("unexpected problems: %+v", p.Problems)
	}
	assertJobs(t, p.Jobs, []*Job{
		{
			Name: "lint", Line: 13, Stage: "test",
			HasRules:      true,
			Rules:         []Rule{{If: `$CI_PIPELINE_SOURCE == "merge_request_event"`}},
			Image:         &Image{Name: "node:22"},
			Interruptible: ptr(true),
		},
		{
			Name: "e2e", Line: 19, Stage: "test",
			Image:         &Image{Name: "mcr.microsoft.com/playwright:v1.60.0"},
			Interruptible: ptr(true),
		},
		{
			Name: "publish", Line: 25, Stage: "deploy",
			Image:         &Image{Name: "node:22-alpine"},
			Interruptible: ptr(true),
		},
		{
			Name: "audit", Line: 30, Stage: "test",
			HasRules: true,
			Rules: []Rule{
				{If: `$CI_PIPELINE_SOURCE == "merge_request_event"`},
				{When: "never"},
			},
		},
	})
}

func TestParseProblems(t *testing.T) {
	tests := []struct {
		name string
		src  string
		// kind and message of the only expected problem; message is a substring.
		kind    string
		message string
		job     string
		line    int
		// jobs are the names of the jobs still returned.
		jobs []string
	}{
		{
			name:    "invalid YAML",
			src:     "build: [\n",
			kind:    KindParseError,
			message: "invalid YAML",
		},
		{
			name:    "unknown anchor",
			src:     "build:\n  script: *missing\n",
			kind:    KindParseError,
			message: "unknown anchor 'missing'",
		},
		{
			name:    "root is not a mapping",
			src:     "- build\n",
			kind:    KindParseError,
			message: "must be a mapping of keywords and jobs",
			line:    1,
		},
		{
			name:    "job is not a mapping",
			src:     "build: make\nlint:\n  script: [x]\n",
			kind:    KindParseError,
			message: `job "build" must be a mapping`,
			job:     "build",
			line:    1,
			jobs:    []string{"lint"},
		},
		{
			name:    "extends cycle",
			src:     ".a:\n  extends: .b\n.b:\n  extends: .a\nbuild:\n  extends: .a\nlint:\n  script: [x]\n",
			kind:    KindParseError,
			message: "extends cycle: .a -> .b -> .a",
			job:     "build",
			line:    5,
			jobs:    []string{"lint"},
		},
		{
			name:    "unknown extends parent",
			src:     "build:\n  extends: .missing\n",
			kind:    KindParseError,
			message: `extends ".missing": no such job or hidden job`,
			job:     "build",
			line:    1,
		},
		{
			name:    "leftover include",
			src:     "include:\n  - local: ci/build.yml\nbuild:\n  script: [x]\n",
			kind:    KindUnresolvedInclude,
			message: "include: is not resolved",
			line:    1,
			jobs:    []string{"build"},
		},
		{
			name:    "workflow is not a mapping",
			src:     "workflow: [x]\n",
			kind:    KindParseError,
			message: "workflow: must be a mapping",
			line:    1,
		},
		{
			name:    "rules is not a list",
			src:     "build:\n  rules: always\n",
			kind:    KindParseError,
			message: "rules: must be a list",
			job:     "build",
			line:    1,
			jobs:    []string{"build"},
		},
		{
			name:    "a rule is not a mapping",
			src:     "build:\n  rules: [always]\n",
			kind:    KindParseError,
			message: "each rule must be a mapping",
			job:     "build",
			line:    1,
			jobs:    []string{"build"},
		},
		{
			name:    "only has the wrong type",
			src:     "build:\n  only: 3\n",
			kind:    KindParseError,
			message: "only: must be a list or a mapping",
			job:     "build",
			line:    1,
			jobs:    []string{"build"},
		},
		{
			name:    "image has the wrong type",
			src:     "build:\n  image: [a, b]\n",
			kind:    KindParseError,
			message: "image: must be a string or a mapping",
			job:     "build",
			line:    1,
			jobs:    []string{"build"},
		},
		{
			name:    "merge key on a scalar",
			src:     "build:\n  <<: 3\n",
			kind:    KindParseError,
			message: "a merge key (<<) must point to a mapping",
		},
		{
			name:    "scalar with an invalid explicit tag",
			src:     "build:\n  timeout: !!int soon\n",
			kind:    KindParseError,
			message: "line 2",
		},
		{
			name:    "invalid scalar inside a list",
			src:     "build:\n  script: [!!int soon]\n",
			kind:    KindParseError,
			message: "line 2",
		},
		{
			name:    "invalid value inside a merged mapping",
			src:     "build:\n  <<: {timeout: !!int soon}\n",
			kind:    KindParseError,
			message: "line 2",
		},
		{
			name:    "merge list holding a scalar",
			src:     "build:\n  <<: [3]\n",
			kind:    KindParseError,
			message: "a merge key (<<) must point to a mapping",
		},
		{
			name:    "reference with a nested list",
			src:     "build:\n  rules:\n    - !reference [[.a]]\n",
			kind:    KindParseError,
			message: "!reference expects a list of keys",
		},
		{
			name:    "empty reference",
			src:     "build:\n  rules: !reference []\n",
			kind:    KindParseError,
			message: "!reference expects a list of keys",
		},
		{
			name:    "reference with a key that is not a string",
			src:     "build:\n  rules: !reference [.rules, 1]\n",
			kind:    KindParseError,
			message: "!reference expects a list of keys",
		},
		{
			name:    "circular references",
			src:     ".a:\n  x: !reference [.b, y]\n.b:\n  y: !reference [.a, x]\nbuild:\n  script: [x]\n",
			kind:    KindUnresolvedReference,
			message: "!reference [.b, y] is part of a circular chain",
			job:     ".a",
			line:    2,
			jobs:    []string{"build"},
		},
		{
			name:    "reference to a null value",
			src:     ".a:\n  x: null\nbuild:\n  rules: !reference [.a, x]\n",
			kind:    KindUnresolvedReference,
			message: "!reference [.a, x] could not be found",
			job:     "build",
			line:    4,
			jobs:    []string{"build"},
		},
		{
			name:    "reference through a list",
			src:     ".a:\n  - x: 1\nbuild:\n  rules: !reference [.a, x]\n",
			kind:    KindUnresolvedReference,
			message: "!reference [.a, x] could not be found",
			job:     "build",
			line:    4,
			jobs:    []string{"build"},
		},
		{
			// References are resolved when the YAML is loaded, before extends:
			// .child has no rules of its own at that point.
			name:    "reference to a key only extends provides",
			src:     ".base:\n  rules: [{when: always}]\n.child:\n  extends: .base\nbuild:\n  rules: !reference [.child, rules]\n",
			kind:    KindUnresolvedReference,
			message: "!reference [.child, rules] could not be found",
			job:     "build",
			line:    6,
			jobs:    []string{"build"},
		},
		{
			name:    "a rule left as an unresolved reference",
			src:     "build:\n  rules:\n    - !reference [.missing]\n    - when: never\n",
			kind:    KindUnresolvedReference,
			message: "!reference [.missing] could not be found",
			job:     "build",
			line:    3,
			jobs:    []string{"build"},
		},
		{
			// The template's broken reference is reached through both jobs,
			// and reported once, against the first key that contains it.
			name:    "a broken reference reached twice is reported once",
			src:     ".tpl:\n  x: !reference [.missing]\na:\n  rules: !reference [.tpl, x]\nb:\n  rules: !reference [.tpl, x]\n",
			kind:    KindUnresolvedReference,
			message: "!reference [.missing] could not be found",
			job:     ".tpl",
			line:    2,
			jobs:    []string{"a", "b"},
		},
		{
			name:    "rules as a whole is a reference",
			src:     "build:\n  rules: !reference [.rules, mr]\n",
			kind:    KindUnresolvedReference,
			message: "!reference [.rules, mr] could not be found; what it stands for is treated as unknown",
			job:     "build",
			line:    2,
			jobs:    []string{"build"},
		},
		{
			name:    "image is a reference",
			src:     "build:\n  image: !reference [.img, image]\n",
			kind:    KindUnresolvedReference,
			message: "!reference [.img, image] could not be found",
			job:     "build",
			line:    2,
			jobs:    []string{"build"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Parse([]byte(tt.src))
			if len(p.Problems) != 1 {
				t.Fatalf("got %d problems %+v, want 1", len(p.Problems), p.Problems)
			}
			got := p.Problems[0]
			if got.Kind != tt.kind || !strings.Contains(got.Message, tt.message) {
				t.Errorf("problem = %+v, want kind %q and message containing %q", got, tt.kind, tt.message)
			}
			if got.Job != tt.job || got.Line != tt.line {
				t.Errorf("problem at job %q line %d, want job %q line %d", got.Job, got.Line, tt.job, tt.line)
			}
			var names []string
			for _, j := range p.Jobs {
				names = append(names, j.Name)
			}
			if !reflect.DeepEqual(names, tt.jobs) {
				t.Errorf("jobs = %v, want %v", names, tt.jobs)
			}
		})
	}
}

func TestParseEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []*Job
	}{
		{name: "empty file", src: "", want: nil},
		{
			name: "only as a single ref, except as a list",
			src:  "build:\n  only: main\n  except: [tags, schedules]\n",
			want: []*Job{{
				Name: "build", Line: 1, Stage: "test",
				Only:   &Filter{Refs: []string{"main"}},
				Except: &Filter{Refs: []string{"tags", "schedules"}},
			}},
		},
		{
			name: "only with changes",
			src:  "build:\n  only:\n    changes: [go.mod]\n",
			want: []*Job{{Name: "build", Line: 1, Stage: "test", Only: &Filter{Changes: true}}},
		},
		{
			name: "legacy top-level image is a default",
			src:  "image: ruby:3.3\nbuild:\n  script: [x]\n",
			want: []*Job{{Name: "build", Line: 2, Stage: "test", Image: &Image{Name: "ruby:3.3"}}},
		},
		{
			name: "default: wins over the legacy top-level image",
			src:  "image: ruby:3.3\ndefault:\n  image: ruby:3.4\nbuild:\n  script: [x]\n",
			want: []*Job{{Name: "build", Line: 4, Stage: "test", Image: &Image{Name: "ruby:3.4"}}},
		},
		{
			name: "image mapping without a name",
			src:  "build:\n  image:\n    entrypoint: [\"\"]\n",
			want: []*Job{{Name: "build", Line: 1, Stage: "test", Image: &Image{}}},
		},
		{
			name: "changes with compare_to",
			src:  "build:\n  rules:\n    - changes:\n        paths: [go.mod]\n        compare_to: main\n    - changes:\n        paths: [go.sum]\n",
			want: []*Job{{
				Name: "build", Line: 1, Stage: "test", HasRules: true,
				Rules: []Rule{{Changes: true, ChangesCompareTo: true}, {Changes: true}},
			}},
		},
		{
			name: "rules: [] is not the same as no rules",
			src:  "build:\n  rules: []\n",
			want: []*Job{{Name: "build", Line: 1, Stage: "test", HasRules: true}},
		},
		{
			name: "nested rule lists are flattened",
			src:  "build:\n  rules:\n    - [{if: $A}, {if: $B}]\n    - when: never\n",
			want: []*Job{{
				Name: "build", Line: 1, Stage: "test", HasRules: true,
				Rules: []Rule{{If: "$A"}, {If: "$B"}, {When: "never"}},
			}},
		},
		{
			name: "a job can extend a visible job",
			src:  "build:\n  stage: build\n  interruptible: false\nbuild-arm:\n  extends: build\n",
			want: []*Job{
				{Name: "build", Line: 1, Stage: "build", Interruptible: ptr(false)},
				{Name: "build-arm", Line: 4, Stage: "build", Interruptible: ptr(false), Extends: []string{"build"}},
			},
		},
		{
			name: "jobs merged in at the top level are jobs, after the written ones",
			src:  ".shared: &shared\n  lint:\n    script: [x]\n  audit:\n    script: [x]\n<<: *shared\nbuild:\n  script: [x]\n",
			want: []*Job{
				{Name: "build", Line: 7, Stage: "test"},
				{Name: "audit", Line: 6, Stage: "test"},
				{Name: "lint", Line: 6, Stage: "test"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Parse([]byte(tt.src))
			if len(p.Problems) != 0 {
				t.Fatalf("unexpected problems: %+v", p.Problems)
			}
			assertJobs(t, p.Jobs, tt.want)
		})
	}
}

func TestDeepMerge(t *testing.T) {
	base := map[string]any{
		"variables": map[string]any{"A": "1", "B": "1"},
		"rules":     []any{"base"},
		"stage":     "build",
	}
	over := map[string]any{
		"variables": map[string]any{"B": "2", "C": "2"},
		"rules":     []any{"over"},
	}
	got := deepMerge(base, over)
	want := map[string]any{
		"variables": map[string]any{"A": "1", "B": "2", "C": "2"},
		"rules":     []any{"over"},
		"stage":     "build",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("deepMerge:\n got  %v\n want %v", got, want)
	}
	if base["variables"].(map[string]any)["B"] != "1" {
		t.Error("deepMerge modified its base")
	}
}

func TestParseReferences(t *testing.T) {
	got := parseFixture(t, "references.gitlab-ci.yml")
	want := parseFixture(t, "references-expanded.gitlab-ci.yml")
	if len(got.Problems) != 0 || len(want.Problems) != 0 {
		t.Fatalf("unexpected problems: %+v %+v", got.Problems, want.Problems)
	}
	// Same jobs as the configuration written out in full; only the lines of
	// the job keys differ between the two files.
	for _, p := range []*Pipeline{got, want} {
		for _, j := range p.Jobs {
			j.Line = 0
		}
	}
	assertJobs(t, got.Jobs, want.Jobs)
}

// resolvedRaw returns the visible top-level keys of a fixture once references
// are resolved, before the typed model: script and the other keys the model
// does not keep are compared too.
func resolvedRaw(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "config", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	raw, err := toMap(doc.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	var order []entry
	for k := range raw {
		order = append(order, entry{name: k})
	}
	p := &parser{pipeline: &Pipeline{}}
	resolved := p.resolveReferences(raw, order)
	if len(p.pipeline.Problems) != 0 {
		t.Fatalf("problems: %+v", p.pipeline.Problems)
	}
	for k := range resolved {
		if strings.HasPrefix(k, ".") {
			delete(resolved, k)
		}
	}
	return resolved
}

func TestResolvedReferencesMatchTheExpandedConfiguration(t *testing.T) {
	got := resolvedRaw(t, "references.gitlab-ci.yml")
	want := resolvedRaw(t, "references-expanded.gitlab-ci.yml")
	// GitLab flattens nested lists in script and rules; compare flattened.
	if !reflect.DeepEqual(flatten(got), flatten(want)) {
		t.Errorf("resolved:\n got  %v\n want %v", got, want)
	}
}

func flatten(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, item := range x {
			out[k] = flatten(item)
		}
		return out
	case []any:
		var out []any
		for _, item := range x {
			if l, ok := flatten(item).([]any); ok {
				out = append(out, l...)
			} else {
				out = append(out, flatten(item))
			}
		}
		return out
	default:
		return v
	}
}
