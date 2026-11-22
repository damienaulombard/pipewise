package rules

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/damienaulombard/pipewise/internal/actions"
	"github.com/damienaulombard/pipewise/internal/config"
)

func TestAnalyzeWorkflow(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "actions")
	read := actions.DirReader(root)
	tests := []struct {
		name string
		file string
		// want lists the findings as rule, severity, job and line.
		want []Finding
	}{
		{"simple workflow", "simple.yml", []Finding{
			{Rule: KindNotEvaluated, Severity: Info},
		}},
		{"local reusable workflow", "caller.yml", []Finding{
			{Rule: KindNotEvaluated, Severity: Info},
		}},
		{"remote reusable workflow", "remote.yml", []Finding{
			{Rule: KindNotEvaluated, Severity: Info},
			{Rule: actions.KindUnresolvedWorkflow, Severity: Warning, Job: "shared", Line: 5},
		}},
		{"invalid workflow", "invalid.yml", []Finding{
			{Rule: KindNotEvaluated, Severity: Info},
			{Rule: config.KindParseError, Severity: Error, Job: "no-runner", Line: 4},
			{Rule: config.KindParseError, Severity: Error, Job: "bad-step", Line: 10},
			{Rule: config.KindParseError, Severity: Error, Job: "good", Line: 12},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", tt.file))
			if err != nil {
				t.Fatal(err)
			}
			o := AnalyzeWorkflow(data, read)
			var got []Finding
			for _, f := range o.Findings {
				if f.Message == "" || f.Explanation == "" {
					t.Errorf("%s: message or explanation missing: %+v", f.Rule, f)
				}
				if f.Rule == config.KindParseError && !strings.HasPrefix(f.Explanation, "GitHub would reject") {
					t.Errorf("parse error explained for GitLab: %q", f.Explanation)
				}
				got = append(got, Finding{Rule: f.Rule, Severity: f.Severity, Job: f.Job, Line: f.Line})
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("findings:\n%+v\nwant\n%+v", got, tt.want)
			}
			if len(o.Jobs) != 0 {
				t.Errorf("jobs = %+v, want no verdicts", o.Jobs)
			}
		})
	}
}

func TestAnalyzeWorkflowNeverPanics(t *testing.T) {
	for _, data := range []string{"", ":", "on: [", "- a\n", "on: push\njobs: 3\n", "jobs:\n  a: *x\n"} {
		o := AnalyzeWorkflow([]byte(data), nil)
		if len(o.Findings) < 2 {
			t.Errorf("%q: findings = %+v, want a parse error and the not-evaluated finding", data, o.Findings)
		}
	}
}
