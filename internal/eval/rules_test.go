package eval

import (
	"strings"
	"testing"

	"github.com/damienaulombard/pipewise/internal/config"
)

// v is a job's verdicts on merge request, default branch and tag.
type v [3]Verdict

const (
	S = Skipped
	R = Runs
	M = Manual
	X = Maybe
)

func evaluate(t *testing.T, yaml string, opts Options) *Result {
	t.Helper()
	p := config.Parse([]byte(yaml))
	for _, pb := range p.Problems {
		if pb.Kind == config.KindParseError {
			t.Fatalf("config problem: %+v", pb)
		}
	}
	if opts.DefaultBranch == "" {
		opts.DefaultBranch = "main"
	}
	return Evaluate(p, opts)
}

// job wraps a job body under the name "job".
func job(body string) string {
	return "job:\n  script: [x]\n" + body
}

func TestRules(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want v
	}{
		{"merge requests only", job(`  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
`), v{R, S, S}},
		{"every event, the usual way", job(`  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
    - if: $CI_COMMIT_TAG
`), v{R, R, R}},
		{"tags only", job(`  rules:
    - if: $CI_COMMIT_TAG
`), v{S, S, R}},
		{"the tag's name is not known", job(`  rules:
    - if: $CI_COMMIT_TAG =~ /^v\d+/
`), v{S, S, X}},
		{"first matching rule wins", job(`  rules:
    - if: $CI_COMMIT_TAG
      when: never
    - when: on_success
`), v{R, R, S}},
		{"rule when overrides the job's when", job(`  when: manual
  rules:
    - if: $CI_COMMIT_TAG
      when: always
    - if: $CI_COMMIT_BRANCH
`), v{S, M, R}},
		{"when values", job(`  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
      when: delayed
    - if: $CI_COMMIT_BRANCH
      when: on_failure
    - when: sometimes
`), v{R, X, X}},
		{"changes is unknown, except on tags", job(`  rules:
    - changes: [go.mod]
`), v{X, X, R}},
		{"changes with compare_to stays unknown on tags", job(`  rules:
    - changes:
        paths: [go.mod]
        compare_to: main
`), v{X, X, X}},
		{"exists is unknown", job(`  rules:
    - exists: [Dockerfile]
`), v{X, X, X}},
		{"if and changes must both match", job(`  rules:
    - if: $CI_COMMIT_TAG
      changes: [go.mod]
`), v{S, S, R}},
		{"an unknown variable", job(`  rules:
    - if: $DEPLOY_ENABLED
`), v{X, X, X}},
		{"an unsure rule whose verdict the rest of the list agrees with", job(`  rules:
    - if: $DEPLOY_ENABLED
    - when: on_success
`), v{R, R, R}},
		{"an unsure rule the rest of the list disagrees with", job(`  rules:
    - if: $DEPLOY_ENABLED
      when: never
    - when: on_success
`), v{X, X, X}},
		{"an unresolved reference", job(`  rules:
    - !reference [.rules, mr]
    - when: never
`), v{X, X, X}},
		{"rules: [] adds the job nowhere", job("  rules: []\n"), v{S, S, S}},

		// Without rules: GitLab's default only: [branches, tags].
		{"no rules, no only", job(""), v{S, R, R}},
		{"no rules, manual", job("  when: manual\n"), v{S, M, M}},
		{"no rules, when: never", job("  when: never\n"), v{S, S, S}},
		{"except alone keeps the default only", job("  except: [tags]\n"), v{S, R, S}},
		{"only merge_requests", job("  only: [merge_requests]\n"), v{R, S, S}},
		{"only tags", job("  only: [tags]\n"), v{S, S, R}},
		{"only branches", job("  only: [branches]\n"), v{S, R, S}},
		{"only pushes", job("  only: [pushes]\n"), v{S, R, R}},
		{"only schedules", job("  only: [schedules]\n"), v{S, S, S}},
		{"only a branch name", job("  only: [main]\n"), v{S, R, X}},
		{"only another branch name", job("  only: [develop]\n"), v{S, S, X}},
		{"only a branch pattern", job("  only: [/^ma/]\n"), v{S, R, X}},
		{"only a pattern matching no default branch", job("  only: [/^release/]\n"), v{S, S, X}},
		{"only a ref in another project", job("  only: [main@group/project]\n"), v{S, X, X}},
		{"only variables", job(`  only:
    variables: [$CI_COMMIT_TAG]
`), v{S, S, R}},
		{"only refs and variables must both match", job(`  only:
    refs: [branches, tags]
    variables: [$CI_COMMIT_BRANCH]
`), v{S, R, S}},
		{"only changes", job(`  only:
    refs: [branches, tags]
    changes: [go.mod]
`), v{S, X, R}},
		{"except changes skips tags", job(`  except:
    changes: [go.mod]
`), v{S, X, S}},
		{"except with any matching key excludes", job(`  only: [branches, tags]
  except:
    refs: [schedules]
    variables: [$CI_COMMIT_TAG]
`), v{S, R, S}},
		{"an unsure only with a manual job", job(`  when: manual
  only:
    variables: [$DEPLOY_ENABLED]
`), v{X, X, X}},
		{"an unsure only with when: never", job(`  when: never
  only:
    variables: [$DEPLOY_ENABLED]
`), v{S, S, S}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := evaluate(t, tt.yaml, Options{})
			if len(res.Problems) != 0 {
				t.Fatalf("problems: %+v", res.Problems)
			}
			if got := v(res.Jobs[0].Verdicts); got != tt.want {
				t.Errorf("verdicts = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		workflow v
		jobs     []v
	}{
		{
			name: "no pipeline on tags",
			yaml: `workflow:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
lint:
  script: [x]
  rules:
    - when: on_success
`,
			workflow: v{R, R, S},
			jobs:     []v{{R, R, S}},
		},
		{
			name: "when: never stops the pipeline",
			yaml: `workflow:
  rules:
    - if: $CI_COMMIT_TAG
      when: never
    - when: always
lint:
  script: [x]
  rules:
    - when: on_success
`,
			workflow: v{R, R, S},
			jobs:     []v{{R, R, S}},
		},
		{
			// workflow: rules removes the default only: [branches, tags], so a
			// job without rules runs in merge request pipelines too.
			name: "a job without rules runs wherever the workflow allows",
			yaml: `workflow:
  rules:
    - when: always
lint:
  script: [x]
`,
			workflow: v{R, R, R},
			jobs:     []v{{R, R, R}},
		},
		{
			name: "an unsure workflow makes running jobs maybe",
			yaml: `workflow:
  rules:
    - if: $RUN_PIPELINE
lint:
  script: [x]
  rules:
    - if: $CI_COMMIT_TAG
`,
			workflow: v{X, X, X},
			jobs:     []v{{S, S, X}},
		},
		{
			name: "workflow without rules",
			yaml: `workflow:
  name: build
lint:
  script: [x]
`,
			workflow: v{R, R, R},
			jobs:     []v{{S, R, R}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := evaluate(t, tt.yaml, Options{})
			if got := v(res.Workflow); got != tt.workflow {
				t.Errorf("workflow = %v, want %v", got, tt.workflow)
			}
			for i, want := range tt.jobs {
				if got := v(res.Jobs[i].Verdicts); got != want {
					t.Errorf("job %d = %v, want %v", i, got, want)
				}
			}
		})
	}
}

func TestSkipBranchPipelinesForMRs(t *testing.T) {
	// The project setting removes the default only, like workflow: rules.
	tests := []struct {
		setting Truth
		want    v
	}{
		{False, v{S, R, R}},
		{True, v{R, R, R}},
		{Unknown, v{X, R, R}},
	}
	for _, tt := range tests {
		t.Run(tt.setting.String(), func(t *testing.T) {
			res := evaluate(t, job(""), Options{SkipBranchPipelinesForMRs: tt.setting})
			if got := v(res.Jobs[0].Verdicts); got != tt.want {
				t.Errorf("verdicts = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultBranchOption(t *testing.T) {
	res := evaluate(t, job("  only: [main]\n"), Options{DefaultBranch: "master"})
	if got := v(res.Jobs[0].Verdicts); got != (v{S, S, X}) {
		t.Errorf("only: [main] with default branch master = %v", got)
	}
	if got := res.Jobs[0].On(DefaultBranch); got != Skipped {
		t.Errorf("On(DefaultBranch) = %v", got)
	}
}

func TestExpressionProblems(t *testing.T) {
	res := evaluate(t, `workflow:
  rules:
    - if: $A ==
lint:
  script: [x]
  rules:
    - if: $A ==
    - if: $CI_COMMIT_BRANCH =~ true
audit:
  script: [x]
  only:
    variables: ["$B !"]
`, Options{})
	var msgs []string
	for _, p := range res.Problems {
		if p.Kind != config.KindParseError {
			t.Errorf("problem kind = %q", p.Kind)
		}
		msgs = append(msgs, p.Job+": "+p.Message)
	}
	got := strings.Join(msgs, "\n")
	want := strings.Join([]string{
		`: invalid expression "$A ==": position 3: == needs an operand on each side`,
		`lint: invalid expression "$A ==": position 3: == needs an operand on each side`,
		`lint: expression "$CI_COMMIT_BRANCH =~ true" cannot be evaluated: position 18: the right side of =~ must be a regular expression`,
	}, "\n")
	if got != want {
		t.Errorf("problems, reported once each:\n%s\nwant:\n%s", got, want)
	}
	// An expression GitLab cannot read counts as false: the workflow never
	// matches, so no pipeline and no job, anywhere.
	if got := v(res.Workflow); got != (v{S, S, S}) {
		t.Errorf("workflow = %v", got)
	}
	// "$B !" is valid in GitLab: (!$B), true when $B is undefined. $B is a
	// project variable here, so audit is unsure; the workflow skips it anyway.
	if got := v(res.Jobs[1].Verdicts); got != (v{S, S, S}) {
		t.Errorf("audit = %v", got)
	}
}

func TestVerdictString(t *testing.T) {
	for verdict, want := range map[Verdict]string{Skipped: "skipped", Runs: "runs", Manual: "manual", Maybe: "maybe"} {
		if got := verdict.String(); got != want {
			t.Errorf("Verdict(%d).String() = %q, want %q", verdict, got, want)
		}
	}
}

func TestTruthLogic(t *testing.T) {
	all := []Truth{False, True, Unknown}
	for _, a := range all {
		if not(not(a)) != a {
			t.Errorf("not(not(%v)) != %v", a, a)
		}
		for _, b := range all {
			// De Morgan holds in Kleene logic.
			if not(and(a, b)) != or(not(a), not(b)) {
				t.Errorf("De Morgan fails for %v, %v", a, b)
			}
		}
	}
}
