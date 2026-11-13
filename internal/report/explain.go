package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/damienaulombard/pipewise/internal/eval"
)

// ExplainText writes, for a person, what decides whether jr's job runs on
// each event.
func ExplainText(w io.Writer, file string, jr eval.JobResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Job %q (%s:%d)\n\n", jr.Job.Name, file, jr.Job.Line)
	for _, ev := range eval.Events {
		prefix := fmt.Sprintf("%-16s %-8s ", ev, jr.On(ev))
		wrap(&b, prefix, printable(phrase(jr.On(ev), jr.Reasons[ev])))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// printable shows the control characters of s as Go escapes, so that a
// configuration cannot send them to the terminal.
func printable(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) {
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// phrase says in one sentence what decided verdict v.
func phrase(v eval.Verdict, why eval.Reason) string {
	var s string
	switch why.Source {
	case eval.SourceRule:
		entry := fmt.Sprintf("rule %d", why.Index)
		if why.Workflow {
			entry = fmt.Sprintf("workflow: rules entry %d", why.Index)
		}
		if why.Sure {
			s = entry + " matched: " + why.Clause
		} else {
			s = entry + " may match: " + why.Clause
			if v != eval.Maybe {
				s += fmt.Sprintf("; %s either way", v)
			}
		}
	case eval.SourceNoRule:
		s = "no rule matched"
		if why.Workflow {
			s = "no workflow: rules entry matched, so no pipeline"
		}
	case eval.SourceOnlyExcept:
		s = "only/except: " + why.Clause
		if !why.Sure {
			s = "only/except may match: " + why.Clause
		}
	default:
		s = "no rules or only: " + why.Clause
		if !why.Sure {
			s += " may apply"
		}
	}
	if len(why.Unknowns) > 0 {
		s += " (depends on " + strings.Join(why.Unknowns, ", ") + ")"
	}
	return s
}

// The JSON explanation. Field names are part of pipewise's public interface.
type jsonExplanation struct {
	Schema int         `json:"schema"`
	File   string      `json:"file"`
	Job    string      `json:"job"`
	Line   int         `json:"line"`
	Events []jsonEvent `json:"events"`
}

type jsonEvent struct {
	Event     string     `json:"event"`
	Verdict   string     `json:"verdict"`
	DecidedBy jsonReason `json:"decided_by"`
}

type jsonReason struct {
	Workflow bool     `json:"workflow"`
	Source   string   `json:"source"`
	Index    int      `json:"index"`
	Clause   string   `json:"clause"`
	Certain  bool     `json:"certain"`
	Unknowns []string `json:"unknowns"`
}

// eventKeys are the events as the JSON reports name them.
var eventKeys = [3]string{"merge_request", "default_branch", "tag"}

// ExplainJSON writes the same for a tool.
func ExplainJSON(w io.Writer, file string, jr eval.JobResult) error {
	r := jsonExplanation{Schema: Schema, File: file, Job: jr.Job.Name, Line: jr.Job.Line}
	for _, ev := range eval.Events {
		why := jr.Reasons[ev]
		unknowns := why.Unknowns
		if unknowns == nil {
			unknowns = []string{}
		}
		r.Events = append(r.Events, jsonEvent{
			Event:   eventKeys[ev],
			Verdict: jr.On(ev).String(),
			DecidedBy: jsonReason{
				Workflow: why.Workflow,
				Source:   why.Source.String(),
				Index:    why.Index,
				Clause:   why.Clause,
				Certain:  why.Sure,
				Unknowns: unknowns,
			},
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}
