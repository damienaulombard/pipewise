package rules

import (
	"fmt"
	"strings"

	"github.com/damienaulombard/pipewise/internal/eval"
)

// e2eEverywhere reports an end-to-end job that surely runs on more than one
// of the events a commit meets. A job is an end-to-end job when its name or
// stage contains one of e2eWords, whatever the case. As for redundant-job,
// only "runs" verdicts count, never "manual" or "maybe".
type e2eEverywhere struct{}

func (e2eEverywhere) ID() string { return "e2e-everywhere" }

var e2eWords = []string{"e2e", "end-to-end", "acceptance", "cypress", "playwright", "selenium"}

func isE2E(name, stage string) bool {
	name, stage = strings.ToLower(name), strings.ToLower(stage)
	for _, w := range e2eWords {
		if strings.Contains(name, w) || strings.Contains(stage, w) {
			return true
		}
	}
	return false
}

func (r e2eEverywhere) Check(a *Analysis) []Finding {
	var out []Finding
	for _, jr := range a.Result.Jobs {
		j := jr.Job
		if j.Trigger || !isE2E(j.Name, j.Stage) {
			continue
		}
		var where []string
		for _, ev := range eval.Events {
			if jr.On(ev) == eval.Runs {
				where = append(where, eventPhrases[ev])
			}
		}
		if len(where) < 2 {
			continue
		}
		out = append(out, Finding{
			Rule:     r.ID(),
			Severity: Warning,
			Job:      j.Name,
			Line:     j.Line,
			Message: fmt.Sprintf("Job %q is an end-to-end suite and runs %d times for the same commit: %s.",
				j.Name, len(where), joinAnd(where)),
			Explanation: "End-to-end suites are usually the slowest jobs of a pipeline, and on the same commit " +
				"they give the same answer on every event: each extra run adds their full duration to the pipeline. " +
				"Run the suite once, on the merge request, where it can still stop the merge; " +
				"if it is too slow for merge requests, run it on the default branch only.",
			Suggestion: suggestion(j.Name, keepRules[eval.MergeRequest], j.Only != nil || j.Except != nil),
		})
	}
	return out
}
