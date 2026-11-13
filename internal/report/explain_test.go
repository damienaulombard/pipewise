package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/damienaulombard/pipewise/internal/eval"
	"github.com/damienaulombard/pipewise/internal/rules"
)

func explained(t *testing.T, job string) eval.JobResult {
	t.Helper()
	return explainedIn(t, "explain.gitlab-ci.yml", job)
}

func explainedIn(t *testing.T, fixture, job string) eval.JobResult {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "explain", fixture))
	if err != nil {
		t.Fatal(err)
	}
	for _, jr := range rules.Analyze(data, eval.Options{DefaultBranch: "main"}).Jobs {
		if jr.Job.Name == job {
			return jr
		}
	}
	t.Fatalf("no job %q in the fixture", job)
	return eval.JobResult{}
}

func TestExplainGolden(t *testing.T) {
	for _, tt := range []struct{ fixture, job, golden string }{
		{"explain.gitlab-ci.yml", "test", "explain-test"},
		{"explain.gitlab-ci.yml", "deploy", "explain-deploy"},
		{"explain.gitlab-ci.yml", "lint", "explain-lint"},
		// workflow: rules decide on two events; no job rule matches on the third.
		{"workflow.gitlab-ci.yml", "build", "workflow-build"},
		// Rules written in a template, reached through extends and !reference.
		{"inherited.gitlab-ci.yml", "unit", "inherited-unit"},
		{"inherited.gitlab-ci.yml", "deploy", "inherited-deploy"},
		{"inherited.gitlab-ci.yml", "release", "inherited-release"},
	} {
		t.Run(tt.golden, func(t *testing.T) {
			jr := explainedIn(t, tt.fixture, tt.job)
			var text, js bytes.Buffer
			if err := ExplainText(&text, ".gitlab-ci.yml", jr); err != nil {
				t.Fatal(err)
			}
			if err := ExplainJSON(&js, ".gitlab-ci.yml", jr); err != nil {
				t.Fatal(err)
			}
			if !json.Valid(js.Bytes()) {
				t.Fatalf("invalid JSON:\n%s", js.String())
			}
			golden(t, filepath.Join("..", "explain", tt.golden+".txt"), text.Bytes())
			golden(t, filepath.Join("..", "explain", tt.golden+".json"), js.Bytes())
		})
	}
}

// TestExplainPhrases covers every way a reason reads in the text output.
func TestExplainPhrases(t *testing.T) {
	tests := []struct {
		name    string
		verdict eval.Verdict
		why     eval.Reason
		want    string
	}{
		{"rule matched", eval.Runs, eval.Reason{Source: eval.SourceRule, Index: 2, Clause: "if: $X", Sure: true},
			"rule 2 matched: if: $X"},
		{"rule may match, same verdict", eval.Runs, eval.Reason{Source: eval.SourceRule, Index: 1, Clause: "if: $X", Unknowns: []string{"$X"}},
			"rule 1 may match: if: $X; runs either way (depends on $X)"},
		{"rule may match", eval.Maybe, eval.Reason{Source: eval.SourceRule, Index: 1, Clause: "changes", Unknowns: []string{"changed files"}},
			"rule 1 may match: changes (depends on changed files)"},
		{"no rule", eval.Skipped, eval.Reason{Source: eval.SourceNoRule},
			"no rule matched"},
		{"workflow entry", eval.Skipped, eval.Reason{Workflow: true, Source: eval.SourceRule, Index: 1, Clause: "if: $CI_COMMIT_TAG, when: never", Sure: true},
			"workflow: rules entry 1 matched: if: $CI_COMMIT_TAG, when: never"},
		{"no workflow entry", eval.Skipped, eval.Reason{Workflow: true, Source: eval.SourceNoRule},
			"no workflow: rules entry matched, so no pipeline"},
		{"only/except", eval.Skipped, eval.Reason{Source: eval.SourceOnlyExcept, Clause: "only: refs [tags]", Sure: true},
			"only/except: only: refs [tags]"},
		{"only/except may match", eval.Maybe, eval.Reason{Source: eval.SourceOnlyExcept, Clause: "only: refs [main]", Unknowns: []string{"the project path", "$A"}},
			"only/except may match: only: refs [main] (depends on the project path, $A)"},
		{"default", eval.Runs, eval.Reason{Source: eval.SourceDefault, Clause: "GitLab's default only: [branches, tags]", Sure: true},
			"no rules or only: GitLab's default only: [branches, tags]"},
		{"default unsure", eval.Maybe, eval.Reason{Source: eval.SourceDefault, Clause: "GitLab's default only: [branches, tags]", Unknowns: []string{"skip_branch_pipelines_for_mrs"}},
			"no rules or only: GitLab's default only: [branches, tags] may apply (depends on skip_branch_pipelines_for_mrs)"},
		{"on failure", eval.Maybe, eval.Reason{Source: eval.SourceRule, Index: 1, Clause: "when: on_failure", Sure: true, Unknowns: []string{"earlier failures"}},
			"rule 1 matched: when: on_failure (depends on earlier failures)"},
		{"rule written under the job", eval.Runs, eval.Reason{Source: eval.SourceRule, Index: 2, Line: 8, Clause: "if: $X", Sure: true},
			"rule 2 (ci.yml:8) matched: if: $X"},
		{"inherited rule", eval.Maybe, eval.Reason{Source: eval.SourceRule, Index: 2, Line: 8, From: ".tpl", Clause: "if: $X", Unknowns: []string{"$X"}},
			"rule 2 (ci.yml:8, from .tpl) may match: if: $X (depends on $X)"},
		{"workflow entry with its line", eval.Skipped, eval.Reason{Workflow: true, Source: eval.SourceRule, Index: 1, Line: 3, Clause: "when: never", Sure: true},
			"workflow: rules entry 1 (ci.yml:3) matched: when: never"},
		{"inherited only", eval.Skipped, eval.Reason{Source: eval.SourceOnlyExcept, Line: 14, From: ".base", Clause: "only: refs [tags]", Sure: true},
			"only/except (ci.yml:14, from .base): only: refs [tags]"},
		{"only next to an inherited except", eval.Skipped, eval.Reason{Source: eval.SourceOnlyExcept, Line: 27, ExceptLine: 20, ExceptFrom: ".never-main",
			Clause: "only: refs [branches]; except: refs [main]", Sure: true},
			"only/except (ci.yml:27): only: refs [branches]; except (ci.yml:20, from .never-main): refs [main]"},
		{"default only next to an except", eval.Runs, eval.Reason{Source: eval.SourceDefault, ExceptLine: 30,
			Clause: "GitLab's default only: [branches, tags]; except: refs [tags]", Sure: true},
			"no rules or only: GitLab's default only: [branches, tags]; except (ci.yml:30): refs [tags]"},
		{"only/except with its line, may match", eval.Maybe, eval.Reason{Source: eval.SourceOnlyExcept, Line: 14, Clause: "only: refs [main]", Unknowns: []string{"$A"}},
			"only/except (ci.yml:14) may match: only: refs [main] (depends on $A)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := phrase("ci.yml", tt.verdict, tt.why); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestExplainTextEscapes keeps control characters from the configuration
// out of the terminal.
func TestExplainTextEscapes(t *testing.T) {
	jr := explained(t, "test")
	jr.Reasons[eval.MergeRequest].Clause = "if: $A == \"\x1b[31mred\""
	var b bytes.Buffer
	if err := ExplainText(&b, ".gitlab-ci.yml", jr); err != nil {
		t.Fatal(err)
	}
	if bytes.ContainsRune(b.Bytes(), 0x1b) {
		t.Errorf("the output holds a raw escape character:\n%q", b.String())
	}
	if !bytes.Contains(b.Bytes(), []byte(`\x1b[31mred`)) {
		t.Errorf("the escape character is not shown as \\x1b:\n%s", b.String())
	}
}

// TestExplainJSONKeepsOperators keeps clauses readable in the JSON: no
// HTML escaping of &, < and >.
func TestExplainJSONKeepsOperators(t *testing.T) {
	jr := explained(t, "test")
	jr.Reasons[eval.MergeRequest].Clause = "if: $A && $B"
	var b bytes.Buffer
	if err := ExplainJSON(&b, "a<b>.yml", jr); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"clause": "if: $A && $B"`, `"file": "a<b>.yml"`} {
		if !bytes.Contains(b.Bytes(), []byte(want)) {
			t.Errorf("the JSON does not hold %s:\n%s", want, b.String())
		}
	}
}

// TestExplainJSONContract pins the field names and shapes consumers rely on.
func TestExplainJSONContract(t *testing.T) {
	var buf bytes.Buffer
	if err := ExplainJSON(&buf, "ci.yml", explained(t, "test")); err != nil {
		t.Fatal(err)
	}
	var r struct {
		Schema int    `json:"schema"`
		File   string `json:"file"`
		Job    string `json:"job"`
		Line   int    `json:"line"`
		Events []struct {
			Event     string `json:"event"`
			Verdict   string `json:"verdict"`
			DecidedBy struct {
				Workflow   bool     `json:"workflow"`
				Source     string   `json:"source"`
				Index      int      `json:"index"`
				Line       int      `json:"line"`
				From       string   `json:"from"`
				ExceptLine int      `json:"except_line"`
				ExceptFrom string   `json:"except_from"`
				Clause     string   `json:"clause"`
				Certain    bool     `json:"certain"`
				Unknowns   []string `json:"unknowns"`
			} `json:"decided_by"`
		} `json:"events"`
	}
	dec := json.NewDecoder(&buf)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		t.Fatal(err)
	}
	if r.Schema != 1 || r.File != "ci.yml" || r.Job != "test" || r.Line != 9 || len(r.Events) != 3 {
		t.Fatalf("header = %+v", r)
	}
	// Rule 1 of job test is written on line 13 of the fixture.
	if by := r.Events[0].DecidedBy; by.Line != 13 || by.From != "" {
		t.Errorf("merge request decided at line %d from %q, want 13 from the job", by.Line, by.From)
	}
	var events []string
	for _, e := range r.Events {
		events = append(events, e.Event)
		if e.DecidedBy.Unknowns == nil {
			t.Errorf("%s: unknowns must be a list, even empty", e.Event)
		}
	}
	if got := events; got[0] != "merge_request" || got[1] != "default_branch" || got[2] != "tag" {
		t.Errorf("events = %v", got)
	}
}
