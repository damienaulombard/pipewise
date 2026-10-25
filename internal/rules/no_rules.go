package rules

import "fmt"

// noRules reports a job with no rules, only or except: GitLab's defaults
// decide where it runs, and those defaults change with configuration the
// job's author may never look at.
type noRules struct{}

func (noRules) ID() string { return "no-rules" }

func (r noRules) Check(a *Analysis) []Finding {
	var out []Finding
	for _, j := range a.Pipeline.Jobs {
		if j.Trigger || j.HasRules || j.Only != nil || j.Except != nil {
			continue
		}
		out = append(out, Finding{
			Rule:     r.ID(),
			Severity: Warning,
			Job:      j.Name,
			Line:     j.Line,
			Message:  fmt.Sprintf("Job %q has no rules, only or except, so GitLab's defaults decide where it runs.", j.Name),
			Explanation: "By default a job runs on every branch and every tag. With workflow: rules, " +
				"or with the project setting that skips branch pipelines for merge requests, " +
				"it runs in every pipeline instead, merge requests included. " +
				"Either way it rarely runs only where it is needed.",
			Suggestion: suggestion(j.Name, keepRules[0], false),
		})
	}
	return out
}
