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
		// redundant sets both preconditions of the redundant-job fix; without
		// it, redundant-job findings are all skipped and left out of want.
		redundant bool
		want      []skip
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
		{name: "redundant-rules", redundant: true},
		{name: "redundant-only", redundant: true},
		{name: "redundant-extends", redundant: true},
		{name: "redundant-skipped", redundant: true, want: []skip{
			{"redundant-job", "merged"},
			{"redundant-job", "anchored"},
			{"redundant-job", "folded"},
			{"redundant-job", "legacy"},
			{"missing-interruptible", "alias"},
			{"redundant-job", "alias"},
			{"redundant-job", "flow"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join("testdata", "fix", tt.name+".gitlab-ci.yml")
			data, err := os.ReadFile(filepath.Join("..", "..", path))
			if err != nil {
				t.Fatal(err)
			}
			o := Options{}
			if tt.redundant {
				o = Options{MergedResultsPipelines: eval.True, ProtectedDefaultBranch: eval.True}
			}
			res := Fix(path, data, rules.Analyze(data, opts), o)

			var got []skip
			for _, s := range res.Skipped {
				if s.Rule == "redundant-job" && !tt.redundant {
					continue
				}
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
				if f.Rule == "redundant-job" && !tt.redundant {
					continue
				}
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
		res := Fix("ci.yml", data, rules.Analyze(data, opts), Options{ExpireIn: tt.expireIn})
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
		res := Fix("ci.yml", data, rules.Analyze(data, opts), Options{})
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
	res := Fix("ci.yml", data, &rules.Outcome{Findings: findings}, Options{})
	if res.Diff != "" {
		t.Errorf("diff = %q", res.Diff)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Job != "gone" {
		t.Errorf("skipped = %+v", res.Skipped)
	}
}

func TestFixKeepsLineEndings(t *testing.T) {
	data := []byte("build:\r\n  script: [make]\r\n  rules:\r\n    - if: $CI_MERGE_REQUEST_IID\r\n")
	res := Fix("ci.yml", data, rules.Analyze(data, opts), Options{})
	want := "build:\r\n  interruptible: true\r\n  script: [make]\r\n"
	if !strings.HasPrefix(string(res.Fixed), want) {
		t.Errorf("fixed = %q, want prefix %q", res.Fixed, want)
	}
}

func TestFixSpecHeader(t *testing.T) {
	data := []byte("spec:\n  inputs:\n    stage: {}\n---\nbuild:\n  script: [make]\n  artifacts:\n    paths: [bin/]\n")
	res := Fix("ci.yml", data, rules.Analyze(data, opts), Options{})
	if !strings.Contains(string(res.Fixed), "  artifacts:\n    expire_in: 1 week\n    paths: [bin/]\n") {
		t.Errorf("fixed = %q", res.Fixed)
	}
}

func TestDiffWithoutTrailingNewline(t *testing.T) {
	data := []byte("a:\n  artifacts:\n    paths: [x]")
	res := Fix("ci.yml", data, rules.Analyze(data, opts), Options{})
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
	got := unified("f.yml", lines, []edit{
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
		"redundant-job":         true,
		"no-rules":              false,
	} {
		if Fixable(id) != want {
			t.Errorf("Fixable(%q) = %v", id, !want)
		}
	}
}

const redundant = "build:\n  script: [make]\n  interruptible: true\n"

func TestFixRedundantPreconditions(t *testing.T) {
	data := []byte(redundant)
	tests := []struct {
		name             string
		merged, protect  eval.Truth
		want, wantReason []string
	}{
		{"both unknown", eval.Unknown, eval.Unknown, nil, []string{
			"merged results pipelines are not known to be enabled (--merged-results-pipelines=unknown)",
			"direct pushes to the default branch are not known to be refused (--protected-default-branch=unknown)",
			"Settings > Merge requests > Merge options",
			"Settings > Repository > Protected branches",
		}},
		{"merged results off", eval.False, eval.True, nil, []string{
			"merged results pipelines are not enabled (--merged-results-pipelines=false)",
		}},
		{"pushes allowed", eval.True, eval.False, nil, []string{
			"direct pushes to the default branch are allowed (--protected-default-branch=false)",
		}},
		{"both met", eval.True, eval.True, []string{
			"+  rules:\n",
			"+    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH\n",
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Fix("ci.yml", data, rules.Analyze(data, opts),
				Options{MergedResultsPipelines: tt.merged, ProtectedDefaultBranch: tt.protect})
			for _, w := range tt.want {
				if !strings.Contains(res.Diff, w) {
					t.Errorf("diff\n%s\nwant a line %q", res.Diff, w)
				}
			}
			if tt.wantReason == nil {
				if len(res.Skipped) != 0 {
					t.Errorf("skipped = %+v", res.Skipped)
				}
				return
			}
			if len(res.Skipped) != 1 || res.Skipped[0].Rule != "redundant-job" || res.Diff != "" {
				t.Fatalf("skipped = %+v, diff %q", res.Skipped, res.Diff)
			}
			for _, w := range tt.wantReason {
				if !strings.Contains(res.Skipped[0].Reason, w) {
					t.Errorf("reason %q\nwant %q", res.Skipped[0].Reason, w)
				}
			}
			if tt.merged == eval.True && strings.Contains(res.Skipped[0].Reason, "merged results") ||
				tt.protect == eval.True && strings.Contains(res.Skipped[0].Reason, "direct pushes") {
				t.Errorf("reason names a precondition that is met: %q", res.Skipped[0].Reason)
			}
		})
	}
}

// The other rules are fixed when redundant-job is skipped.
func TestFixRedundantSkippedOthersFixed(t *testing.T) {
	data := []byte("build:\n  script: [make]\n  artifacts:\n    paths: [bin/]\n")
	res := Fix("ci.yml", data, rules.Analyze(data, opts), Options{})
	if !strings.Contains(res.Diff, "+    expire_in: 1 week\n") || len(res.Skipped) != 1 {
		t.Errorf("diff\n%s\nskipped %+v", res.Diff, res.Skipped)
	}
}

var met = Options{MergedResultsPipelines: eval.True, ProtectedDefaultBranch: eval.True}

func TestFixRedundantReplace(t *testing.T) {
	tests := map[string]struct{ src, want string }{
		"rules right below the job, with interruptible": {
			"unit:\n  rules:\n    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n    - if: $CI_COMMIT_TAG\n  script: [make]\n",
			"unit:\n  interruptible: true\n  rules:\n    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n  script: [make]\n",
		},
		"last job, trailing comment kept": {
			"a:\n  script: [a]\n  interruptible: true\n  rules:\n    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n    - if: $CI_COMMIT_TAG\n\n# end\n",
			"a:\n  script: [a]\n  interruptible: true\n  rules:\n    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n\n# end\n",
		},
		"multi-line plain scalar": {
			"a:\n  script: [a]\n  interruptible: true\n  rules:\n    - if: $CI_PIPELINE_SOURCE ==\n        \"merge_request_event\"\n    - if: $CI_COMMIT_TAG\nb:\n  script: [b]\n  interruptible: true\n  rules:\n    - if: $CI_MERGE_REQUEST_IID\n",
			"a:\n  script: [a]\n  interruptible: true\n  rules:\n    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\nb:\n  script: [b]\n  interruptible: true\n  rules:\n    - if: $CI_MERGE_REQUEST_IID\n",
		},
		"CRLF": {
			"a:\r\n  script: [a]\r\n  interruptible: true\r\n  only: [branches, tags]\r\n",
			"a:\r\n  script: [a]\r\n  interruptible: true\r\n  rules:\r\n    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH\r\n",
		},
		"sequence at the key's indentation": {
			"a:\n  script: [a]\n  interruptible: true\n  rules:\n  - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n  - if: $CI_COMMIT_TAG\n  stage: test\n",
			"a:\n  script: [a]\n  interruptible: true\n  rules:\n    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n  stage: test\n",
		},
		"no trailing newline": {
			"a:\n  script: [a]\n  interruptible: true\n  except: [schedules]",
			"a:\n  script: [a]\n  interruptible: true\n  rules:\n    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH\n",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			data := []byte(tt.src)
			res := Fix("ci.yml", data, rules.Analyze(data, opts), met)
			if string(res.Fixed) != tt.want || len(res.Skipped) != 0 {
				t.Errorf("fixed = %q, skipped %+v\nwant %q\ndiff:\n%s", res.Fixed, res.Skipped, tt.want, res.Diff)
			}
		})
	}
}

// A redundant-job finding whose job is not in the outcome, or that no
// longer runs twice, is skipped.
func TestFixRedundantStale(t *testing.T) {
	data := []byte(redundant)
	findings := []rules.Finding{{Rule: "redundant-job", Job: "build", Line: 1}}
	res := Fix("ci.yml", data, &rules.Outcome{Findings: findings}, met)
	if res.Diff != "" || len(res.Skipped) != 1 {
		t.Errorf("no job: diff %q, skipped %+v", res.Diff, res.Skipped)
	}

	outcome := rules.Analyze(data, opts)
	outcome.Jobs[0].Verdicts = [3]eval.Verdict{eval.Skipped, eval.Maybe, eval.Manual}
	res = Fix("ci.yml", data, outcome, met)
	if res.Diff != "" || len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "does not surely run") {
		t.Errorf("no longer runs: diff %q, skipped %+v", res.Diff, res.Skipped)
	}
}

// A second fix of the same lines is skipped, and the first one kept.
func TestFixRedundantTwice(t *testing.T) {
	data := []byte("a:\n  script: [a]\n  interruptible: true\n  only: [branches, tags]\n")
	outcome := rules.Analyze(data, opts)
	outcome.Findings = append(outcome.Findings, outcome.Findings...)
	res := Fix("ci.yml", data, outcome, met)
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != errOverlap.Error() ||
		!strings.Contains(res.Diff, "+  rules:\n") {
		t.Errorf("diff\n%s\nskipped %+v", res.Diff, res.Skipped)
	}
}

// The end of an entry that a line less indented than its key continues is
// not guessed.
func TestFixRedundantUnclearEnd(t *testing.T) {
	data := []byte("a:\n  script: [a]\n  interruptible: true\n  rules: [{if: $CI_PIPELINE_SOURCE == \"merge_request_event\"},\n {if: $CI_COMMIT_TAG}]\n")
	res := Fix("ci.yml", data, rules.Analyze(data, opts), met)
	if res.Diff != "" || len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "cannot tell where the rules: of \"a\" ends (line 5)") {
		t.Errorf("diff %q, skipped %+v", res.Diff, res.Skipped)
	}
}

func TestDiffReplace(t *testing.T) {
	lines := strings.SplitAfter(strings.Repeat("x\n", 10), "\n")
	lines = lines[:10]
	got := unified("f.yml", lines, []edit{
		{after: 2, del: 2, text: []string{"a\n"}},
		{after: 9, del: 1},
	})
	want := `--- a/f.yml
+++ b/f.yml
@@ -1,10 +1,8 @@
 x
 x
-x
-x
+a
 x
 x
 x
 x
 x
-x
`
	if got != want {
		t.Errorf("diff =\n%s\nwant\n%s", got, want)
	}
}

// Two fixes cannot touch the same lines; the later one is skipped.
func TestPlaceOverlap(t *testing.T) {
	tests := []struct {
		name     string
		a, b     edit
		conflict bool
	}{
		{"insert and replace at one place", edit{after: 1, text: []string{"i\n"}}, edit{after: 1, del: 2, text: []string{"r\n"}}, false},
		{"two replacements at one place", edit{after: 1, del: 1}, edit{after: 1, del: 2}, true},
		{"insert inside a replacement", edit{after: 1, del: 3}, edit{after: 2}, true},
		{"replacement over an insertion", edit{after: 2}, edit{after: 1, del: 3}, true},
		{"insert right after a replacement", edit{after: 1, del: 2}, edit{after: 3}, false},
		{"overlapping replacements", edit{after: 1, del: 3}, edit{after: 3, del: 2}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := map[int]edit{}
			if err := place(edits, tt.a); err != nil {
				t.Fatal(err)
			}
			if err := place(edits, tt.b); (err != nil) != tt.conflict {
				t.Errorf("place = %v, conflict %v", err, tt.conflict)
			}
		})
	}
}

// A rule that sets more than if: is not dropped silently: the job is left
// as it is.
func TestFixRedundantRuleKeys(t *testing.T) {
	tests := map[string]string{
		"variables": "  rules:\n    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n      variables: {TARGET: review}\n    - if: $CI_COMMIT_TAG\n",
		"when":      "  rules:\n    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n    - if: $CI_COMMIT_TAG\n      when: always\n",
		"alias":     "  rules: *r\n",
	}
	for name, rulesYAML := range tests {
		t.Run(name, func(t *testing.T) {
			data := []byte(".r: &r\n  - if: $CI_MERGE_REQUEST_IID\n    allow_failure: true\n  - if: $CI_COMMIT_TAG\n" +
				"a:\n  script: [a]\n  interruptible: true\n" + rulesYAML)
			res := Fix("ci.yml", data, rules.Analyze(data, opts), met)
			if res.Diff != "" || len(res.Skipped) != 1 ||
				!strings.Contains(res.Skipped[0].Reason, `a rule of "a" sets more than if:`) {
				t.Errorf("diff %q, skipped %+v", res.Diff, res.Skipped)
			}
		})
	}
}

// Each job of redundant-skipped is left for its own reason.
func TestFixRedundantSkipReasons(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "fix", "redundant-skipped.gitlab-ci.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"merged":   `the rules: of "merged" comes from a merge key`,
		"anchored": `the rules: of "anchored" holds the anchor &mine`,
		"folded":   `the rules: of "folded" holds a block scalar`,
		"legacy":   `"legacy" gets only: through extends`,
		"alias":    `"alias" is an alias of another mapping`,
		"flow":     `job "flow" is written on one line`,
	}
	for _, s := range Fix(path, data, rules.Analyze(data, opts), met).Skipped {
		if s.Rule == "redundant-job" && !strings.Contains(s.Reason, want[s.Job]) {
			t.Errorf("%s: reason %q, want %q", s.Job, s.Reason, want[s.Job])
		}
	}
}

// A rules: given by !reference is not read item by item; a rule given by an
// alias is read where its anchor is written.
func TestFixRedundantRulesShape(t *testing.T) {
	data := []byte(".r:\n  rules:\n    - if: $CI_MERGE_REQUEST_IID\n    - if: $CI_COMMIT_TAG\n.i: &i {if: $CI_COMMIT_TAG}\n" +
		"a:\n  script: [a]\n  interruptible: true\n  rules: !reference [.r, rules]\n" +
		"b:\n  script: [b]\n  interruptible: true\n  rules:\n    - if: $CI_MERGE_REQUEST_IID\n    - *i\n")
	res := Fix("ci.yml", data, rules.Analyze(data, opts), met)
	if len(res.Skipped) != 1 || res.Skipped[0].Job != "a" ||
		!strings.Contains(res.Skipped[0].Reason, `the rules: of "a" is not a list written in this file`) {
		t.Errorf("skipped %+v", res.Skipped)
	}
	if !strings.Contains(res.Diff, "-    - *i\n") {
		t.Errorf("diff\n%s", res.Diff)
	}
}

// A job that another job needs stays in every pipeline it is in: dropping
// it from one would make GitLab reject that pipeline.
func TestFixRedundantNeeded(t *testing.T) {
	const a = "a:\n  script: [a]\n  interruptible: true\n  rules:\n    - if: $CI_PIPELINE_SOURCE == \"merge_request_event\"\n    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH\n"
	tests := map[string]string{
		"needs":        "b:\n  script: [b]\n  needs: [a]\n",
		"needs job":    "b:\n  script: [b]\n  needs:\n    - job: a\n      artifacts: false\n",
		"dependencies": "b:\n  script: [b]\n  dependencies: [a]\n",
		"parent":       ".b:\n  needs: [a]\nb:\n  extends: .b\n  script: [b]\n",
	}
	for name, other := range tests {
		t.Run(name, func(t *testing.T) {
			data := []byte(a + other)
			var skipped []Skip
			for _, s := range Fix("ci.yml", data, rules.Analyze(data, opts), met).Skipped {
				if s.Job == "a" {
					skipped = append(skipped, s)
				}
			}
			if len(skipped) != 1 || !strings.Contains(skipped[0].Reason, `"a" is in the needs: or dependencies: of "`) {
				t.Errorf("skipped %+v", skipped)
			}
		})
	}
	// Another job of the same name in a needs: of a pipeline: is not a job
	// of this file.
	data := []byte(a + "b:\n  script: [b]\n  needs:\n    - pipeline: other\n      job: x\n")
	if res := Fix("ci.yml", data, rules.Analyze(data, opts), met); !strings.Contains(res.Diff, "-  rules:\n") {
		t.Errorf("needs of another job: diff\n%s\nskipped %+v", res.Diff, res.Skipped)
	}
}

func TestFixRedundantKeyOrder(t *testing.T) {
	tests := map[string]struct{ src, want, reason string }{
		"except before only": {
			src:  "a:\n  script: [a]\n  except: [schedules]\n  interruptible: true\n  only: [branches, tags]\n",
			want: "a:\n  script: [a]\n  rules:\n    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH\n  interruptible: true\n",
		},
		"except through extends": {
			src:    ".p:\n  except: [schedules]\na:\n  extends: .p\n  script: [a]\n  interruptible: true\n",
			reason: `"a" gets except: through extends`,
		},
		"continuation at the key's indentation": {
			src:    "a:\n  script: [a]\n  interruptible: true\n  rules: [{if: $CI_PIPELINE_SOURCE == \"merge_request_event\"},\n  {if: $CI_COMMIT_TAG}]\n",
			reason: `cannot tell where the rules: of "a" ends (line 5)`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			data := []byte(tt.src)
			res := Fix("ci.yml", data, rules.Analyze(data, opts), met)
			if tt.reason != "" {
				if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, tt.reason) {
					t.Errorf("skipped %+v, want %q", res.Skipped, tt.reason)
				}
				return
			}
			if string(res.Fixed) != tt.want || len(res.Skipped) != 0 {
				t.Errorf("fixed = %q, skipped %+v\nwant %q", res.Fixed, res.Skipped, tt.want)
			}
		})
	}
}
