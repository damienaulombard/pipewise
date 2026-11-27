package rules

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damienaulombard/pipewise/internal/eval"
)

// FuzzAnalyze checks that no GitLab configuration makes Analyze panic: an
// invalid one becomes a finding. `go test` runs the seeds; explore further
// with `go test -fuzz=FuzzAnalyze ./internal/rules`.
func FuzzAnalyze(f *testing.F) {
	// Every YAML file of the test data, GitHub Actions workflows included:
	// for Analyze they are just more YAML.
	err := filepath.WalkDir(filepath.Join("..", "..", "testdata"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yml") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f.Add(data)
		return nil
	})
	if err != nil {
		f.Fatal(err)
	}
	// The inputs of TestAnalyzeWorkflowNeverPanics.
	for _, s := range []string{"", ":", "on: [", "- a\n", "on: push\njobs: 3\n", "jobs:\n  a: *x\n"} {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		for _, skip := range []eval.Truth{eval.True, eval.False, eval.Unknown} {
			if o := Analyze(data, eval.Options{DefaultBranch: "main", SkipBranchPipelinesForMRs: skip}); o == nil {
				t.Fatalf("SkipBranchPipelinesForMRs %v: Analyze returned nil", skip)
			}
		}
	})
}
