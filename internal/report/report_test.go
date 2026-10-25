package report

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/damienaulombard/pipewise/internal/eval"
	"github.com/damienaulombard/pipewise/internal/rules"
)

// Regenerate the golden files with: go test ./internal/report -update
var update = flag.Bool("update", false, "rewrite the golden files")

func outcome(t *testing.T, name string) *rules.Outcome {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "report", name))
	if err != nil {
		t.Fatal(err)
	}
	return rules.Analyze(data, eval.Options{DefaultBranch: "main"})
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "report", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden file:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func TestGolden(t *testing.T) {
	for _, name := range []string{"all-rules", "clean"} {
		t.Run(name, func(t *testing.T) {
			o := outcome(t, name+".gitlab-ci.yml")
			var text, js bytes.Buffer
			if err := Text(&text, ".gitlab-ci.yml", o); err != nil {
				t.Fatal(err)
			}
			if err := JSON(&js, ".gitlab-ci.yml", o); err != nil {
				t.Fatal(err)
			}
			if !json.Valid(js.Bytes()) {
				t.Fatalf("invalid JSON:\n%s", js.String())
			}
			golden(t, name+".txt", text.Bytes())
			golden(t, name+".json", js.Bytes())
		})
	}
}

// TestJSONContract pins the field names and shapes consumers rely on.
func TestJSONContract(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, "ci.yml", outcome(t, "all-rules.gitlab-ci.yml")); err != nil {
		t.Fatal(err)
	}
	var r map[string]any
	if err := json.Unmarshal(buf.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r["schema"] != float64(1) || r["file"] != "ci.yml" {
		t.Errorf("schema/file = %v / %v", r["schema"], r["file"])
	}
	keys := func(m any) string {
		var ks []string
		for k := range m.(map[string]any) {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return strings.Join(ks, ",")
	}
	if got := keys(r); got != "file,findings,jobs,schema,summary" {
		t.Errorf("top-level keys = %s", got)
	}
	f := r["findings"].([]any)[0]
	if got := keys(f); got != "explanation,line,message,rule,severity,suggestion" {
		t.Errorf("keys of a finding without a job = %s", got)
	}
	j := r["jobs"].([]any)[0]
	if got := keys(j); got != "line,name,verdicts" {
		t.Errorf("job keys = %s", got)
	}
	if got := keys(j.(map[string]any)["verdicts"]); got != "default_branch,merge_request,tag" {
		t.Errorf("verdict keys = %s", got)
	}
	if got := keys(r["summary"]); got != "error,info,warning" {
		t.Errorf("summary keys = %s", got)
	}
	if strings.Contains(buf.String(), "\\u003c") || !strings.Contains(buf.String(), "<digest>") {
		t.Error("placeholders such as <digest> are escaped")
	}
}

func TestEmptyListsAreArrays(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, "ci.yml", &rules.Outcome{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"findings": []`) || !strings.Contains(buf.String(), `"jobs": []`) {
		t.Errorf("empty lists are not []:\n%s", buf.String())
	}
}

func TestSummaryLine(t *testing.T) {
	f := func(s ...rules.Severity) []rules.Finding {
		var out []rules.Finding
		for _, sev := range s {
			out = append(out, rules.Finding{Severity: sev})
		}
		return out
	}
	tests := []struct {
		findings []rules.Finding
		want     string
	}{
		{nil, "ci.yml: no findings.\n"},
		{f(rules.Error), "ci.yml: 1 finding: 1 error, 0 warnings, 0 info.\n"},
		{f(rules.Warning, rules.Warning, rules.Info), "ci.yml: 3 findings: 0 errors, 2 warnings, 1 info.\n"},
	}
	for _, tt := range tests {
		if got := summaryLine("ci.yml", tt.findings); got != tt.want {
			t.Errorf("summaryLine = %q, want %q", got, tt.want)
		}
	}
}

func TestWrap(t *testing.T) {
	var b strings.Builder
	wrap(&b, "  why: ", strings.Repeat("word ", 30))
	for _, line := range strings.Split(strings.TrimRight(b.String(), "\n"), "\n") {
		if len(line) > textWidth {
			t.Errorf("line longer than %d: %q", textWidth, line)
		}
		if !strings.HasPrefix(line, "  why: ") && !strings.HasPrefix(line, "       ") {
			t.Errorf("continuation not indented: %q", line)
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestWriteErrors(t *testing.T) {
	o := &rules.Outcome{}
	if err := Text(failingWriter{}, "ci.yml", o); err == nil {
		t.Error("Text: want the writer's error")
	}
	if err := JSON(failingWriter{}, "ci.yml", o); err == nil {
		t.Error("JSON: want the writer's error")
	}
}
