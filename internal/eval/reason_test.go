package eval

import (
	"reflect"
	"testing"
)

// r3 is a job's reasons on merge request, default branch and tag.
type r3 [3]Reason

func TestReasons(t *testing.T) {
	mrRule := `if: $CI_PIPELINE_SOURCE == "merge_request_event"`
	tests := []struct {
		name string
		yaml string
		opts Options
		want r3
	}{
		{"first matching rule, or none", job(`  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - if: $CI_COMMIT_TAG
      when: manual
`), Options{}, r3{
			{Source: SourceRule, Index: 1, Clause: mrRule, Sure: true},
			{Source: SourceNoRule, Sure: true},
			{Source: SourceRule, Index: 2, Clause: "if: $CI_COMMIT_TAG, when: manual", Sure: true},
		}},
		{"a rule that may match, with the same verdict either way", job(`  rules:
    - if: $DEPLOY
    - when: on_success
`), Options{}, r3{
			{Source: SourceRule, Index: 1, Clause: "if: $DEPLOY", Unknowns: []string{"$DEPLOY"}},
			{Source: SourceRule, Index: 1, Clause: "if: $DEPLOY", Unknowns: []string{"$DEPLOY"}},
			{Source: SourceRule, Index: 1, Clause: "if: $DEPLOY", Unknowns: []string{"$DEPLOY"}},
		}},
		{"changes, exists and a reference", job(`  rules:
    - changes: [go.mod]
      exists: [Dockerfile]
      when: never
    - !reference [.rules, deploy]
`), Options{}, r3{
			{Source: SourceRule, Index: 1, Clause: "changes, exists, when: never", Unknowns: []string{"changed files", "files in the repository"}},
			{Source: SourceRule, Index: 1, Clause: "changes, exists, when: never", Unknowns: []string{"changed files", "files in the repository"}},
			{Source: SourceRule, Index: 1, Clause: "changes, exists, when: never", Unknowns: []string{"files in the repository"}},
		}},
		{"an unresolved reference", job(`  rules:
    - !reference [.rules, deploy]
`), Options{}, r3{
			{Source: SourceRule, Index: 1, Clause: "!reference .rules.deploy", Unknowns: []string{"!reference .rules.deploy"}},
			{Source: SourceRule, Index: 1, Clause: "!reference .rules.deploy", Unknowns: []string{"!reference .rules.deploy"}},
			{Source: SourceRule, Index: 1, Clause: "!reference .rules.deploy", Unknowns: []string{"!reference .rules.deploy"}},
		}},
		{"a rule that runs only after a failure", job(`  rules:
    - when: on_failure
`), Options{}, r3{
			{Source: SourceRule, Index: 1, Clause: "when: on_failure", Sure: true, Unknowns: []string{"earlier failures"}},
			{Source: SourceRule, Index: 1, Clause: "when: on_failure", Sure: true, Unknowns: []string{"earlier failures"}},
			{Source: SourceRule, Index: 1, Clause: "when: on_failure", Sure: true, Unknowns: []string{"earlier failures"}},
		}},
		{"workflow rules decide before the job", `workflow:
  rules:
    - if: $CI_COMMIT_TAG
      when: never
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - changes: [src/**]
` + job(`  rules:
    - when: on_success
`), Options{}, r3{
			{Source: SourceRule, Index: 1, Clause: "when: on_success", Sure: true},
			{Workflow: true, Source: SourceRule, Index: 3, Clause: "changes", Unknowns: []string{"changed files"}},
			{Workflow: true, Source: SourceRule, Index: 1, Clause: "if: $CI_COMMIT_TAG, when: never", Sure: true},
		}},
		{"workflow rules that match nothing", `workflow:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
` + job(`  rules:
    - when: never
`), Options{}, r3{
			{Source: SourceRule, Index: 1, Clause: "when: never", Sure: true},
			{Workflow: true, Source: SourceNoRule, Sure: true},
			{Workflow: true, Source: SourceNoRule, Sure: true},
		}},
		{"only and except", job(`  only:
    refs: [main, tags]
    variables: [$DEPLOY]
  except:
    changes: [docs/**]
`), Options{}, r3{
			{Source: SourceOnlyExcept, Clause: "only: refs [main, tags], variables [$DEPLOY]; except: changes", Sure: true},
			{Source: SourceOnlyExcept, Clause: "only: refs [main, tags], variables [$DEPLOY]; except: changes", Unknowns: []string{"$DEPLOY", "changed files"}},
			{Source: SourceOnlyExcept, Clause: "only: refs [main, tags], variables [$DEPLOY]; except: changes", Sure: true},
		}},
		{"refs GitLab cannot settle here", job(`  only: [/^release-/, main@group/app]
`), Options{}, r3{
			{Source: SourceOnlyExcept, Clause: "only: refs [/^release-/, main@group/app]", Sure: true},
			{Source: SourceOnlyExcept, Clause: "only: refs [/^release-/, main@group/app]", Unknowns: []string{"the project path"}},
			{Source: SourceOnlyExcept, Clause: "only: refs [/^release-/, main@group/app]", Unknowns: []string{"the tag's name", "the project path"}},
		}},
		{"default only, kept", job(""), Options{SkipBranchPipelinesForMRs: False}, r3{
			{Source: SourceDefault, Clause: "GitLab's default only: [branches, tags]", Sure: true},
			{Source: SourceDefault, Clause: "GitLab's default only: [branches, tags]", Sure: true},
			{Source: SourceDefault, Clause: "GitLab's default only: [branches, tags]", Sure: true},
		}},
		{"default only, removed by workflow rules", "workflow:\n  rules:\n    - when: always\n" + job(""), Options{}, r3{
			{Source: SourceDefault, Clause: "GitLab's default only is removed by workflow: rules", Sure: true},
			{Source: SourceDefault, Clause: "GitLab's default only is removed by workflow: rules", Sure: true},
			{Source: SourceDefault, Clause: "GitLab's default only is removed by workflow: rules", Sure: true},
		}},
		{"default only, removed by the project setting", job(""), Options{SkipBranchPipelinesForMRs: True}, r3{
			{Source: SourceDefault, Clause: "GitLab's default only is removed by skip_branch_pipelines_for_mrs", Sure: true},
			{Source: SourceDefault, Clause: "GitLab's default only is removed by skip_branch_pipelines_for_mrs", Sure: true},
			{Source: SourceDefault, Clause: "GitLab's default only is removed by skip_branch_pipelines_for_mrs", Sure: true},
		}},
		{"default only, project setting unknown", job(""), Options{SkipBranchPipelinesForMRs: Unknown}, r3{
			{Source: SourceDefault, Clause: "GitLab's default only: [branches, tags]", Unknowns: []string{"skip_branch_pipelines_for_mrs"}},
			{Source: SourceDefault, Clause: "GitLab's default only: [branches, tags]", Sure: true},
			{Source: SourceDefault, Clause: "GitLab's default only: [branches, tags]", Sure: true},
		}},
		{"an if: that depends on inputs", job(`  rules:
    - if: $[[ inputs.env ]] == "prod"
`), Options{}, r3{
			{Source: SourceRule, Index: 1, Clause: `if: $[[ inputs.env ]] == "prod"`, Unknowns: []string{"inputs"}},
			{Source: SourceRule, Index: 1, Clause: `if: $[[ inputs.env ]] == "prod"`, Unknowns: []string{"inputs"}},
			{Source: SourceRule, Index: 1, Clause: `if: $[[ inputs.env ]] == "prod"`, Unknowns: []string{"inputs"}},
		}},
		{"default only removed, with except", "workflow:\n  rules:\n    - when: always\n" + job(`  except: [tags]
`), Options{}, r3{
			{Source: SourceDefault, Clause: "GitLab's default only is removed by workflow: rules; except: refs [tags]", Sure: true},
			{Source: SourceDefault, Clause: "GitLab's default only is removed by workflow: rules; except: refs [tags]", Sure: true},
			{Source: SourceDefault, Clause: "GitLab's default only is removed by workflow: rules; except: refs [tags]", Sure: true},
		}},
		{"default only kept, with except, on failure", job(`  when: on_failure
  except: [tags]
`), Options{SkipBranchPipelinesForMRs: False}, r3{
			{Source: SourceDefault, Clause: "GitLab's default only: [branches, tags]; except: refs [tags]", Sure: true},
			{Source: SourceDefault, Clause: "GitLab's default only: [branches, tags]; except: refs [tags]", Sure: true, Unknowns: []string{"earlier failures"}},
			{Source: SourceDefault, Clause: "GitLab's default only: [branches, tags]; except: refs [tags]", Sure: true},
		}},
		{"default only with except, project setting unknown", job(`  except:
    changes: [docs/**]
`), Options{SkipBranchPipelinesForMRs: Unknown}, r3{
			{Source: SourceDefault, Clause: "GitLab's default only: [branches, tags]; except: changes",
				Unknowns: []string{"changed files", "skip_branch_pipelines_for_mrs"}},
			{Source: SourceDefault, Clause: "GitLab's default only: [branches, tags]; except: changes",
				Unknowns: []string{"changed files"}},
			{Source: SourceDefault, Clause: "GitLab's default only: [branches, tags]; except: changes", Sure: true},
		}},
		{"an if: GitLab rejects", job(`  rules:
    - if: $A ==
`), Options{}, r3{
			{Source: SourceNoRule, Sure: true},
			{Source: SourceNoRule, Sure: true},
			{Source: SourceNoRule, Sure: true},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := evaluate(t, tt.yaml, tt.opts)
			got := r3(res.Jobs[0].Reasons)
			for _, ev := range Events {
				// Where the clause is written is TestReasonPositions' concern.
				got[ev].Line, got[ev].From = 0, ""
				got[ev].ExceptLine, got[ev].ExceptFrom = 0, ""
				if !reflect.DeepEqual(got[ev], tt.want[ev]) {
					t.Errorf("%s:\n got %+v\nwant %+v", ev, got[ev], tt.want[ev])
				}
			}
		})
	}
}

func TestSourceString(t *testing.T) {
	for s, want := range map[Source]string{
		SourceRule: "rule", SourceNoRule: "no-rule", SourceOnlyExcept: "only-except", SourceDefault: "default",
	} {
		if got := s.String(); got != want {
			t.Errorf("Source(%d) = %q, want %q", s, got, want)
		}
	}
}

// TestReasonPositions carries where the deciding clause is written.
func TestReasonPositions(t *testing.T) {
	res := evaluate(t, `.tests:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
workflow:
  rules:
    - if: $CI_COMMIT_TAG
      when: never
    - when: always
job:
  extends: .tests
  script: [x]
deploy:
  script: [x]
  only: [tags]
lint:
  script: [x]
.tags-only:
  only: [tags]
.never-main:
  except: [main]
inherited-only:
  extends: .tags-only
  script: [x]
both:
  extends: .never-main
  script: [x]
  only: [branches]
except-only:
  script: [x]
  except: [tags]
`, Options{})
	type at struct {
		line       int
		from       string
		exceptLine int
		exceptFrom string
	}
	want := map[string][3]at{
		"job":            {{3, ".tests", 0, ""}, {}, {6, "", 0, ""}},
		"deploy":         {{14, "", 0, ""}, {14, "", 0, ""}, {6, "", 0, ""}},
		"lint":           {{}, {}, {6, "", 0, ""}},
		"inherited-only": {{18, ".tags-only", 0, ""}, {18, ".tags-only", 0, ""}, {6, "", 0, ""}},
		"both":           {{27, "", 20, ".never-main"}, {27, "", 20, ".never-main"}, {6, "", 0, ""}},
		"except-only":    {{0, "", 30, ""}, {0, "", 30, ""}, {6, "", 0, ""}},
	}
	for _, jr := range res.Jobs {
		var got [3]at
		for _, ev := range Events {
			why := jr.Reasons[ev]
			got[ev] = at{why.Line, why.From, why.ExceptLine, why.ExceptFrom}
		}
		if got != want[jr.Job.Name] {
			t.Errorf("%s: got %+v, want %+v", jr.Job.Name, got, want[jr.Job.Name])
		}
	}
	if len(res.Jobs) != len(want) {
		t.Errorf("%d jobs, want %d", len(res.Jobs), len(want))
	}
}

// TestReasonExceptPosition covers except next to GitLab's default only,
// without workflow: rules, whether the project setting is known or not.
func TestReasonExceptPosition(t *testing.T) {
	yaml := `.never-tags:
  except: [tags]
plain:
  extends: .never-tags
  script: [x]
`
	for _, setting := range []Truth{False, Unknown} {
		res := evaluate(t, yaml, Options{SkipBranchPipelinesForMRs: setting})
		for _, ev := range Events {
			why := res.Jobs[0].Reasons[ev]
			if why.Source != SourceDefault || why.Line != 0 || why.ExceptLine != 2 || why.ExceptFrom != ".never-tags" {
				t.Errorf("setting %v, %s: %+v", setting, ev, why)
			}
		}
	}
}
