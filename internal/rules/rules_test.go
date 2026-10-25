package rules

import (
	"strings"
	"testing"

	"github.com/damienaulombard/pipewise/internal/eval"
)

func analyze(yaml string) []Finding {
	return Analyze([]byte(yaml), eval.Options{DefaultBranch: "main"})
}

func only(findings []Finding, rule string) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

func TestRedundantJob(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		message string // empty when no finding is expected
		suggest string
	}{
		{
			name: "merge request and default branch",
			yaml: `unit:
  script: [go test ./...]
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
`,
			message: `Job "unit" runs 2 times for the same commit: on the merge request and on the default branch.`,
			suggest: "unit:\n  rules:\n    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n",
		},
		{
			name: "all three events",
			yaml: `unit:
  script: [go test ./...]
  rules:
    - when: on_success
`,
			message: `Job "unit" runs 3 times for the same commit: on the merge request, on the default branch and on the tag.`,
			suggest: "unit:\n  rules:\n    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n",
		},
		{
			// GitLab's default only: [branches, tags]: the job is kept on the
			// first event it runs on, the default branch.
			name:    "default branch and tag, through only",
			yaml:    "build:\n  script: [make]\n  only: [branches, tags]\n",
			message: `Job "build" runs 2 times for the same commit: on the default branch and on the tag.`,
			suggest: "build:\n  # replaces only: and except:, which cannot be used with rules:\n" +
				"  rules:\n    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH\n",
		},
		{
			name: "once, on merge requests",
			yaml: `unit:
  script: [go test ./...]
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
`,
		},
		{
			name: "manual on the tag does not count",
			yaml: `deploy:
  script: [make deploy]
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
    - if: $CI_COMMIT_TAG
      when: manual
`,
		},
		{
			// changes is unknown on merge requests and the default branch:
			// those verdicts are "maybe", and only the tag is sure.
			name: "maybe does not count",
			yaml: `unit:
  script: [go test ./...]
  rules:
    - changes: [go.mod]
`,
		},
		{
			name: "trigger jobs are skipped",
			yaml: `downstream:
  trigger:
    project: group/other
  rules:
    - when: on_success
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := only(analyze(tt.yaml), "redundant-job")
			if tt.message == "" {
				if len(got) != 0 {
					t.Fatalf("unexpected findings: %+v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("got %d findings %+v, want 1", len(got), got)
			}
			f := got[0]
			if f.Message != tt.message {
				t.Errorf("message:\n got  %s\n want %s", f.Message, tt.message)
			}
			if f.Suggestion != tt.suggest {
				t.Errorf("suggestion:\n got  %q\n want %q", f.Suggestion, tt.suggest)
			}
			if f.Severity != Error || f.Line != 1 || f.Job == "" || f.Explanation == "" {
				t.Errorf("finding = %+v", f)
			}
		})
	}
}

func TestProblemsBecomeFindings(t *testing.T) {
	findings := analyze(`include:
  - local: ci/build.yml
lint:
  script: [x]
  rules:
    - !reference [.missing]
    - if: $A ==
`)
	type want struct {
		rule     string
		severity Severity
		line     int
		job      string
	}
	var got []want
	for _, f := range findings {
		got = append(got, want{f.Rule, f.Severity, f.Line, f.Job})
		if f.Explanation == "" || f.Message == "" {
			t.Errorf("finding without message or explanation: %+v", f)
		}
	}
	// Sorted by line: the include, then the job's rules.
	expected := []want{
		{"unresolved-include", Warning, 1, ""},
		{"parse-error", Error, 3, "lint"},
		{"unresolved-reference", Warning, 6, "lint"},
	}
	if len(got) != len(expected) {
		t.Fatalf("findings = %+v, want %+v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Errorf("finding %d = %+v, want %+v", i, got[i], expected[i])
		}
	}
}

func TestFindingsAreSorted(t *testing.T) {
	findings := analyze(`b:
  script: [x]
  rules: [{when: on_success}]
a:
  script: [x]
  rules: [{when: on_success}]
`)
	var jobs []string
	for _, f := range findings {
		jobs = append(jobs, f.Job)
	}
	if strings.Join(jobs, ",") != "b,a" {
		t.Errorf("order = %v, want b (line 1) before a (line 4)", jobs)
	}
}

func TestSeverityString(t *testing.T) {
	for s, want := range map[Severity]string{Info: "info", Warning: "warning", Error: "error"} {
		if got := s.String(); got != want {
			t.Errorf("Severity(%d).String() = %q, want %q", s, got, want)
		}
	}
	if Error <= Warning || Warning <= Info {
		t.Error("severities are not ordered")
	}
}

func TestRuleIDsAreStable(t *testing.T) {
	var ids []string
	for _, r := range All() {
		ids = append(ids, r.ID())
	}
	if got := strings.Join(ids, ","); got != "redundant-job" {
		t.Errorf("rule ids = %s", got)
	}
}

func TestFindingsOnTheSameLine(t *testing.T) {
	// Jobs merged in at the top level share the line of the `<<`; a broken
	// expression and a redundancy can land on the same job line.
	findings := analyze(`.shared: &shared
  zeta:
    script: [x]
    rules: [{when: on_success}]
  alpha:
    script: [x]
    rules:
      - if: $A ==
      - when: on_success
<<: *shared
`)
	var got []string
	for _, f := range findings {
		got = append(got, f.Rule+"/"+f.Job)
	}
	want := "parse-error/alpha,redundant-job/alpha,redundant-job/zeta"
	if strings.Join(got, ",") != want {
		t.Errorf("order = %v, want %s", got, want)
	}
	for _, f := range findings {
		if f.Line != 10 {
			t.Errorf("%s on line %d, want 10", f.Rule, f.Line)
		}
	}
}
