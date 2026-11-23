package fix

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damienaulombard/pipewise/internal/eval"
	"github.com/damienaulombard/pipewise/internal/rules"
)

var update = flag.Bool("update", false, "rewrite the golden diffs in testdata/fix")

var opts = eval.Options{DefaultBranch: "main", SkipBranchPipelinesForMRs: eval.Unknown}

// skip is the part of a Skip these tests compare.
type skip struct {
	Rule string
	Job  string
}

func TestFixGolden(t *testing.T) {
	tests := []struct {
		name string
		want []skip
	}{
		{name: "interruptible"},
		{name: "artifacts"},
		{name: "extends"},
		{name: "anchor"},
		{name: "skipped", want: []skip{
			{"artifacts-no-expiry", "merged"},
			{"artifacts-no-expiry", "alias"},
			{"missing-interruptible", "alias"},
			{"artifacts-no-expiry", "flow"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join("testdata", "fix", tt.name+".gitlab-ci.yml")
			data, err := os.ReadFile(filepath.Join("..", "..", path))
			if err != nil {
				t.Fatal(err)
			}
			res := Fix(path, data, rules.Analyze(data, opts).Findings, Options{})

			var got []skip
			for _, s := range res.Skipped {
				got = append(got, skip{s.Rule, s.Job})
				if s.Reason == "" || s.Line == 0 {
					t.Errorf("skip %+v has no reason or line", s)
				}
			}
			if len(got) != len(tt.want) {
				t.Fatalf("skipped %+v, want %+v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("skip %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}

			golden := filepath.Join("..", "..", "testdata", "fix", tt.name+".diff")
			if *update {
				if err := os.WriteFile(golden, []byte(res.Diff), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if res.Diff != string(want) {
				t.Errorf("diff differs from %s:\n%s", golden, res.Diff)
			}

			// Running lint on the fixed file no longer reports what was fixed.
			skipped := map[skip]bool{}
			for _, s := range got {
				skipped[s] = true
			}
			for _, f := range rules.Analyze(res.Fixed, opts).Findings {
				if Fixable(f.Rule) && !skipped[skip{f.Rule, f.Job}] {
					t.Errorf("still reported after the fix: %s %q line %d", f.Rule, f.Job, f.Line)
				}
				if f.Rule == "parse-error" {
					t.Errorf("the fixed file does not parse: %s", f.Message)
				}
			}
		})
	}
}

func TestFixExpireIn(t *testing.T) {
	data := []byte("build:\n  script: [make]\n  artifacts:\n    paths: [bin/]\n")
	tests := []struct {
		expireIn string
		want     string
	}{
		{"", "+    expire_in: 1 week\n"},
		{"3 days", "+    expire_in: 3 days\n"},
		{"never: ever", "+    expire_in: 'never: ever'\n"},
	}
	for _, tt := range tests {
		res := Fix("ci.yml", data, rules.Analyze(data, opts).Findings, Options{ExpireIn: tt.expireIn})
		if !strings.Contains(res.Diff, tt.want) {
			t.Errorf("ExpireIn %q: diff\n%s\nwant a line %q", tt.expireIn, res.Diff, tt.want)
		}
	}
}

func TestFixNothingToDo(t *testing.T) {
	tests := map[string]string{
		"no finding":    "build:\n  script: [make]\n",
		"invalid YAML":  "build: [\n",
		"not a mapping": "- a\n",
		"empty":         "",
	}
	for name, src := range tests {
		data := []byte(src)
		res := Fix("ci.yml", data, rules.Analyze(data, opts).Findings, Options{})
		if res.Diff != "" || string(res.Fixed) != src {
			t.Errorf("%s: diff %q, fixed %q", name, res.Diff, res.Fixed)
		}
	}
}

// Findings of other rules, or that no longer match the file, are left alone.
func TestFixIgnoresOtherFindings(t *testing.T) {
	data := []byte("build:\n  script: [make]\n")
	findings := []rules.Finding{
		{Rule: "no-rules", Job: "build", Line: 1},
		{Rule: "missing-interruptible", Job: "gone", Line: 9},
	}
	res := Fix("ci.yml", data, findings, Options{})
	if res.Diff != "" {
		t.Errorf("diff = %q", res.Diff)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Job != "gone" {
		t.Errorf("skipped = %+v", res.Skipped)
	}
}

func TestFixKeepsLineEndings(t *testing.T) {
	data := []byte("build:\r\n  script: [make]\r\n  rules:\r\n    - if: $CI_MERGE_REQUEST_IID\r\n")
	res := Fix("ci.yml", data, rules.Analyze(data, opts).Findings, Options{})
	want := "build:\r\n  interruptible: true\r\n  script: [make]\r\n"
	if !strings.HasPrefix(string(res.Fixed), want) {
		t.Errorf("fixed = %q, want prefix %q", res.Fixed, want)
	}
}

func TestFixSpecHeader(t *testing.T) {
	data := []byte("spec:\n  inputs:\n    stage: {}\n---\nbuild:\n  script: [make]\n  artifacts:\n    paths: [bin/]\n")
	res := Fix("ci.yml", data, rules.Analyze(data, opts).Findings, Options{})
	if !strings.Contains(string(res.Fixed), "  artifacts:\n    expire_in: 1 week\n    paths: [bin/]\n") {
		t.Errorf("fixed = %q", res.Fixed)
	}
}

func TestDiffWithoutTrailingNewline(t *testing.T) {
	data := []byte("a:\n  artifacts:\n    paths: [x]")
	res := Fix("ci.yml", data, rules.Analyze(data, opts).Findings, Options{})
	want := `--- a/ci.yml
+++ b/ci.yml
@@ -1,3 +1,4 @@
 a:
   artifacts:
+    expire_in: 1 week
     paths: [x]
\ No newline at end of file
`
	if res.Diff != want {
		t.Errorf("diff =\n%s\nwant\n%s", res.Diff, want)
	}
}

func TestDiffHunks(t *testing.T) {
	lines := strings.SplitAfter(strings.Repeat("x\n", 20), "\n")
	lines = lines[:20]
	got := unified("f.yml", lines, []insertion{
		{after: 1, text: []string{"a\n"}},
		{after: 3, text: []string{"b\n"}},
		{after: 15, text: []string{"c\n"}},
	})
	want := `--- a/f.yml
+++ b/f.yml
@@ -1,6 +1,8 @@
 x
+a
 x
 x
+b
 x
 x
 x
@@ -13,6 +15,7 @@
 x
 x
 x
+c
 x
 x
 x
`
	if got != want {
		t.Errorf("diff =\n%s\nwant\n%s", got, want)
	}
}

func TestFixable(t *testing.T) {
	for id, want := range map[string]bool{
		"missing-interruptible": true,
		"artifacts-no-expiry":   true,
		"redundant-job":         false,
	} {
		if Fixable(id) != want {
			t.Errorf("Fixable(%q) = %v", id, !want)
		}
	}
}
