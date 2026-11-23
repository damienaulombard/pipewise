package rules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damienaulombard/pipewise/internal/eval"
)

func TestStagesWithoutNeeds(t *testing.T) {
	tests := []struct {
		fixture string
		// message is empty when nothing is reported.
		message string
		line    int
		suggest string
	}{
		{
			fixture: "without-needs",
			message: `3 jobs declare no needs:, so each waits for every job of the earlier stages: ` +
				`"unit", "lint" and "deploy".`,
			line: 2,
			suggest: "unit:\n  needs: [build]\n" +
				"lint:\n  needs: [<jobs it uses>]\n" +
				"deploy:\n  needs: [<jobs it uses>]\n",
		},
		{
			fixture: "partial",
			message: `Job "lint" declares no needs:, so it waits for every job of the earlier stages.`,
			line:    15,
			suggest: "lint:\n  needs: []\n",
		},
		{
			fixture: "first-stage-unused",
			message: `Job "release" declares no needs:, so it waits for every job of the earlier stages.`,
			line:    3,
			suggest: "release:\n  needs: [<jobs it uses>]\n",
		},
		{fixture: "with-needs"},
		{fixture: "needs-empty"},
		{fixture: "single-stage"},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rules", "stages-without-needs", tt.fixture+".gitlab-ci.yml"))
			if err != nil {
				t.Fatal(err)
			}
			got := only(Analyze(data, eval.Options{DefaultBranch: "main"}).Findings, "stages-without-needs")
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
			if f.Severity != Info || f.Job != "" || f.Line != tt.line || f.Message != tt.message ||
				f.Suggestion != tt.suggest || f.Explanation == "" {
				t.Errorf("got %+v\nwant line %d message %q\nsuggestion %q", f, tt.line, tt.message, tt.suggest)
			}
		})
	}
}
