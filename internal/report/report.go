// Package report renders the outcome of an analysis as text for people and
// as JSON for tools.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/damienaulombard/pipewise/internal/eval"
	"github.com/damienaulombard/pipewise/internal/rules"
)

// Schema is the version of the JSON report. It changes with any change a
// consumer could break on.
const Schema = 1

// textWidth is where explanations wrap.
const textWidth = 80

// Text writes o for a person: each finding with its explanation and fix,
// then a summary.
func Text(w io.Writer, file string, o *rules.Outcome) error {
	var b strings.Builder
	for _, f := range o.Findings {
		where := file
		if f.Line > 0 {
			where = fmt.Sprintf("%s:%d", file, f.Line)
		}
		fmt.Fprintf(&b, "%s: %s %s\n", where, f.Severity, f.Rule)
		wrap(&b, "  ", f.Message)
		if f.Explanation != "" {
			wrap(&b, "  why: ", f.Explanation)
		}
		if f.Suggestion != "" {
			b.WriteString("  fix:\n")
			for _, line := range strings.Split(strings.TrimRight(f.Suggestion, "\n"), "\n") {
				fmt.Fprintf(&b, "    %s\n", line)
			}
		}
		b.WriteString("\n")
	}
	b.WriteString(summaryLine(file, o.Findings))
	_, err := io.WriteString(w, b.String())
	return err
}

func summaryLine(file string, findings []rules.Finding) string {
	if len(findings) == 0 {
		return file + ": no findings.\n"
	}
	c := count(findings)
	noun := "findings"
	if len(findings) == 1 {
		noun = "finding"
	}
	return fmt.Sprintf("%s: %d %s: %s, %s, %d info.\n", file, len(findings), noun,
		plural(c.Error, "error"), plural(c.Warning, "warning"), c.Info)
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// wrap writes text after prefix, wrapped at textWidth; continuation lines
// are indented to the width of prefix.
func wrap(b *strings.Builder, prefix, text string) {
	indent := strings.Repeat(" ", len(prefix))
	line := prefix
	for i, word := range strings.Fields(text) {
		if i > 0 && len(line)+1+len(word) > textWidth {
			b.WriteString(line + "\n")
			line = indent + word
			continue
		}
		if i > 0 {
			line += " "
		}
		line += word
	}
	b.WriteString(line + "\n")
}

// summary counts findings by severity.
type summary struct {
	Error   int `json:"error"`
	Warning int `json:"warning"`
	Info    int `json:"info"`
}

func count(findings []rules.Finding) summary {
	var s summary
	for _, f := range findings {
		switch f.Severity {
		case rules.Error:
			s.Error++
		case rules.Warning:
			s.Warning++
		default:
			s.Info++
		}
	}
	return s
}

// The JSON report. Field names are part of pipewise's public interface.
type jsonReport struct {
	Schema   int           `json:"schema"`
	File     string        `json:"file"`
	Findings []jsonFinding `json:"findings"`
	Jobs     []jsonJob     `json:"jobs"`
	Summary  summary       `json:"summary"`
}

type jsonFinding struct {
	Rule        string `json:"rule"`
	Severity    string `json:"severity"`
	Job         string `json:"job,omitempty"`
	Line        int    `json:"line"`
	Message     string `json:"message"`
	Explanation string `json:"explanation"`
	Suggestion  string `json:"suggestion"`
}

type jsonJob struct {
	Name     string       `json:"name"`
	Line     int          `json:"line"`
	Verdicts jsonVerdicts `json:"verdicts"`
}

type jsonVerdicts struct {
	MergeRequest  string `json:"merge_request"`
	DefaultBranch string `json:"default_branch"`
	Tag           string `json:"tag"`
}

// JSON writes o for a tool.
func JSON(w io.Writer, file string, o *rules.Outcome) error {
	r := jsonReport{
		Schema:   Schema,
		File:     file,
		Findings: []jsonFinding{},
		Jobs:     []jsonJob{},
		Summary:  count(o.Findings),
	}
	for _, f := range o.Findings {
		r.Findings = append(r.Findings, jsonFinding{
			Rule:        f.Rule,
			Severity:    f.Severity.String(),
			Job:         f.Job,
			Line:        f.Line,
			Message:     f.Message,
			Explanation: f.Explanation,
			Suggestion:  f.Suggestion,
		})
	}
	for _, jr := range o.Jobs {
		r.Jobs = append(r.Jobs, jsonJob{
			Name: jr.Job.Name,
			Line: jr.Job.Line,
			Verdicts: jsonVerdicts{
				MergeRequest:  jr.On(eval.MergeRequest).String(),
				DefaultBranch: jr.On(eval.DefaultBranch).String(),
				Tag:           jr.On(eval.Tag).String(),
			},
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// Suggestions hold placeholders such as <digest>: keep them readable.
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}
