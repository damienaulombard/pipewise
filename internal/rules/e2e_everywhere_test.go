package rules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damienaulombard/pipewise/internal/eval"
)

func TestE2EEverywhere(t *testing.T) {
	tests := []struct {
		fixture string
		// message is empty when nothing is reported.
		message string
		suggest string
	}{
		{
			fixture: "name",
			message: `Job "test:e2e" is an end-to-end suite and runs 2 times for the same commit: ` +
				`on the merge request and on the default branch.`,
			suggest: "test:e2e:\n  rules:\n    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n",
		},
		{
			fixture: "stage",
			message: `Job "browser" is an end-to-end suite and runs 3 times for the same commit: ` +
				`on the merge request, on the default branch and on the tag.`,
			suggest: "browser:\n  rules:\n    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n",
		},
		{
			fixture: "case",
			message: `Job "Playwright" is an end-to-end suite and runs 2 times for the same commit: ` +
				`on the default branch and on the tag.`,
			suggest: "Playwright:\n  # replaces only: and except:, which cannot be used with rules:\n" +
				"  rules:\n    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n",
		},
		{fixture: "not-e2e"},
		{fixture: "single-event"},
		{fixture: "manual-maybe"},
		{fixture: "trigger"},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rules", "e2e-everywhere", tt.fixture+".gitlab-ci.yml"))
			if err != nil {
				t.Fatal(err)
			}
			got := only(Analyze(data, eval.Options{DefaultBranch: "main"}).Findings, "e2e-everywhere")
			if tt.message == "" {
				if len(got) != 0 {
					t.Fatalf("got %+v, want nothing", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("got %d findings %+v, want 1", len(got), got)
			}
			f := got[0]
			if f.Severity != Warning || f.Line == 0 || f.Message != tt.message ||
				f.Suggestion != tt.suggest || f.Explanation == "" {
				t.Errorf("got %+v\nwant message %q\nsuggestion %q", f, tt.message, tt.suggest)
			}
		})
	}
}

// The job redundant-job reports is reported here too: the fixes differ.
func TestE2EEverywhereWithRedundantJob(t *testing.T) {
	findings := analyze("e2e:\n  script: [x]\n  rules:\n    - when: on_success\n")
	if len(only(findings, "e2e-everywhere")) != 1 || len(only(findings, "redundant-job")) != 1 {
		t.Errorf("findings = %+v", findings)
	}
}

// Every word of the list makes an e2e job, by name or by stage.
func TestIsE2E(t *testing.T) {
	for _, tt := range []struct {
		name, stage string
		want        bool
	}{
		{"e2e", "", true},
		{"End-To-End", "", true},
		{"acceptance", "", true},
		{"run-cypress", "", true},
		{"PLAYWRIGHT", "", true},
		{"selenium-grid", "", true},
		{"browser", "E2E", true},
		{"browser", "end-to-end", true},
		{"browser", "acceptance", true},
		{"browser", "cypress", true},
		{"browser", "playwright", true},
		{"browser", "Selenium", true},
		{"unit", "test", false},
		{"", "", false},
	} {
		if got := isE2E(tt.name, tt.stage); got != tt.want {
			t.Errorf("isE2E(%q, %q) = %v, want %v", tt.name, tt.stage, got, tt.want)
		}
	}
}
