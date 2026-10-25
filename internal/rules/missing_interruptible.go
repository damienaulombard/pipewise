package rules

import (
	"fmt"

	"github.com/damienaulombard/pipewise/internal/eval"
)

// missingInterruptible reports a job that runs on merge requests without
// setting interruptible. An explicit `interruptible: false` is a decision,
// such as for a deployment, and is not reported.
type missingInterruptible struct{}

func (missingInterruptible) ID() string { return "missing-interruptible" }

func (r missingInterruptible) Check(a *Analysis) []Finding {
	var out []Finding
	for _, jr := range a.Result.Jobs {
		j := jr.Job
		if j.Trigger || j.Interruptible != nil || jr.On(eval.MergeRequest) != eval.Runs {
			continue
		}
		out = append(out, Finding{
			Rule:     r.ID(),
			Severity: Info,
			Job:      j.Name,
			Line:     j.Line,
			Message:  fmt.Sprintf("Job %q runs on merge requests but does not set interruptible.", j.Name),
			Explanation: "When a new commit is pushed to a merge request, GitLab can cancel the pipeline " +
				"of the previous commit, but a job that has started is cancelled only if it is interruptible. " +
				"This one keeps running for a commit nobody will merge. " +
				"Setting interruptible: true under default: covers every job at once.",
			Suggestion: fmt.Sprintf("%s:\n  interruptible: true\n", j.Name),
		})
	}
	return out
}
