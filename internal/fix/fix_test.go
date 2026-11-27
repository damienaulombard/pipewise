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

// skip is the part of a Skip these tests compare: the reason in full, since
// lint --fix prints it to the user.
type skip struct {
	Rule   string
	Job    string
	Reason string
}

// skips returns the part of each Skip these tests compare, and checks that
// every skip points at a line.
func skips(t *testing.T, res Result) []skip {
	t.Helper()
	var got []skip
	for _, s := range res.Skipped {
		got = append(got, skip{s.Rule, s.Job, s.Reason})
		if s.Line == 0 {
			t.Errorf("skip %+v has no line", s)
		}
	}
	return got
}

// compareSkips reports the differences between the skips got and want.
func compareSkips(t *testing.T, got, want []skip) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("skipped %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("skip %d = %+v, want %+v", i, got[i], want[i])
		}
	}
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
			{"artifacts-no-expiry", "merged",
				`the artifacts of "merged" come from a merge key (<<:); fix the mapping it merges`},
			{"artifacts-no-expiry", "alias",
				`"alias" is an alias of another mapping, shared with whatever else uses that anchor`},
			{"missing-interruptible", "alias",
				`"alias" is an alias of another mapping, shared with whatever else uses that anchor`},
			{"artifacts-no-expiry", "flow",
				`the artifacts of "flow" are written on one line ({...}); adding a key would rewrite it`},
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

			got := skips(t, res)
			compareSkips(t, got, tt.want)

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
			type ruleJob struct{ rule, job string }
			skipped := map[ruleJob]bool{}
			for _, s := range got {
				skipped[ruleJob{s.Rule, s.Job}] = true
			}
			for _, f := range rules.Analyze(res.Fixed, opts).Findings {
				if Fixable(f.Rule) && !skipped[ruleJob{f.Rule, f.Job}] {
					t.Errorf("still reported after the fix: %s %q line %d", f.Rule, f.Job, f.Line)
				}
				if f.Rule == "parse-error" {
					t.Errorf("the fixed file does not parse: %s", f.Message)
				}
			}
		})
	}
}

// TestFixSkipReasons gives each reason Fix can skip a finding for a
// configuration in testdata/fix/skip that lint reports that way. The file is
// left as it is: none of these findings can be fixed.
func TestFixSkipReasons(t *testing.T) {
	tests := []struct {
		name string
		want []skip
	}{
		{"job-merge-key", []skip{
			{"missing-interruptible", "build", `"build" comes from a merge key (<<:); fix the mapping it merges`},
		}},
		{"anchor-not-under-key", []skip{
			{"artifacts-no-expiry", "build", `the artifacts of "build" are an alias of an anchor that is not under a key`},
		}},
		{"default-merge-key", []skip{
			{"artifacts-no-expiry", "", "default: is not a mapping written in this file"},
		}},
		{"default-artifacts-merge-key", []skip{
			{"artifacts-no-expiry", "", "the default artifacts come from a merge key (<<:); fix the mapping it merges"},
		}},
		{"default-artifacts-reference", []skip{
			{"artifacts-no-expiry", "", "the default artifacts are not a mapping"},
		}},
		{"artifacts-reference", []skip{
			{"artifacts-no-expiry", "build", `the artifacts of "build" are not a mapping`},
		}},
		{"job-reference", []skip{
			{"artifacts-no-expiry", "build", `"build" is not a mapping`},
			{"missing-interruptible", "build", `job "build" is not a mapping`},
		}},
		{"parent-reference", []skip{
			{"artifacts-no-expiry", "build", `".parent" is not a mapping`},
		}},
		{"extends-too-deep", []skip{
			{"artifacts-no-expiry", "build", "extends is nested too deeply"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fix", "skip", tt.name+".gitlab-ci.yml"))
			if err != nil {
				t.Fatal(err)
			}
			res := Fix("ci.yml", data, rules.Analyze(data, opts).Findings, Options{})
			compareSkips(t, skips(t, res), tt.want)
			if res.Diff != "" {
				t.Errorf("diff = %q, want none", res.Diff)
			}
		})
	}
}

// TestFixSkipReasonsNotFromLint covers the reasons no configuration makes
// rules.Analyze produce, with findings written by hand: a file Fix cannot
// read is one Analyze cannot read either, and Analyze reports no fixable
// finding on a job whose extends parent is not in the file, nor on default:
// without artifacts.
func TestFixSkipReasonsNotFromLint(t *testing.T) {
	included := []byte("include:\n  - local: templates.yml\n\nincluded:\n  extends: .from-include\n  script: [make build]\n")
	tests := []struct {
		name     string
		data     []byte
		findings []rules.Finding
		want     []skip
	}{
		{
			name: "unreadable file, job",
			data: []byte("build: [\n"),
			findings: []rules.Finding{
				{Rule: "missing-interruptible", Job: "build", Line: 1},
				{Rule: "artifacts-no-expiry", Job: "build", Line: 1},
			},
			want: []skip{
				{"missing-interruptible", "build", "the configuration could not be read"},
				{"artifacts-no-expiry", "build", "the configuration could not be read"},
			},
		},
		{
			name:     "unreadable file, default",
			data:     []byte("build: [\n"),
			findings: []rules.Finding{{Rule: "artifacts-no-expiry", Line: 1}},
			want:     []skip{{"artifacts-no-expiry", "", "the configuration could not be read"}},
		},
		{
			name:     "job not in the file",
			data:     included,
			findings: []rules.Finding{{Rule: "missing-interruptible", Job: "from-include", Line: 1}},
			want: []skip{{"missing-interruptible", "from-include",
				`"from-include" is not defined in this file; it may come from an included file`}},
		},
		{
			name:     "extends parent from an include",
			data:     included,
			findings: []rules.Finding{{Rule: "artifacts-no-expiry", Job: "included", Line: 4}},
			want: []skip{{"artifacts-no-expiry", "included",
				`".from-include" is not defined in this file; it may come from an included file`}},
		},
		{
			name:     "default without artifacts",
			data:     []byte("default:\n  interruptible: true\nbuild:\n  script: [make]\n"),
			findings: []rules.Finding{{Rule: "artifacts-no-expiry", Line: 1}},
			want:     []skip{{"artifacts-no-expiry", "", "default: has no artifacts written in this file"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Fix("ci.yml", tt.data, tt.findings, Options{})
			compareSkips(t, skips(t, res), tt.want)
			if res.Diff != "" {
				t.Errorf("diff = %q, want none", res.Diff)
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
