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
