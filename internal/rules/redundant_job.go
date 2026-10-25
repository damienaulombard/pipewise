package rules

import (
	"fmt"
	"strings"

	"github.com/damienaulombard/pipewise/internal/eval"
)

// redundantJob reports a job that surely runs on more than one of the
// events a commit meets: the merge request, the default branch, the tag.
//
// Only "runs" verdicts count. A manual job does not run on its own, and a
// "maybe" is not certain: this rule never reports a redundancy it cannot
// prove from the configuration.
type redundantJob struct{}

func (redundantJob) ID() string { return "redundant-job" }

// eventPhrases and keepRules are indexed by eval.Event.
var eventPhrases = [...]string{"on the merge request", "on the default branch", "on the tag"}

var keepRules = [...]string{
	`$CI_PIPELINE_SOURCE == "merge_request_event"`,
	`$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH`,
	`$CI_COMMIT_TAG`,
}

func (r redundantJob) Check(a *Analysis) []Finding {
	var out []Finding
	for _, jr := range a.Result.Jobs {
		if jr.Job.Trigger {
			continue
		}
		var runs []eval.Event
		for _, ev := range eval.Events {
			if jr.On(ev) == eval.Runs {
				runs = append(runs, ev)
			}
		}
		if len(runs) < 2 {
			continue
		}
		where := make([]string, len(runs))
		for i, ev := range runs {
			where[i] = eventPhrases[ev]
		}
		out = append(out, Finding{
			Rule:     r.ID(),
			Severity: Error,
			Job:      jr.Job.Name,
			Line:     jr.Job.Line,
			Message: fmt.Sprintf("Job %q runs %d times for the same commit: %s.",
				jr.Job.Name, len(runs), joinAnd(where)),
			Explanation: "With merged results pipelines and no direct pushes to the default branch, " +
				"the merge request pipeline already tests the exact commit that lands on the default branch, " +
				"and a tag only names that commit. Running the job again there cannot find anything new; " +
				"it only costs runner time and delays the release.",
			Suggestion: suggestion(jr.Job.Name, keepRules[runs[0]], jr.Job.Only != nil || jr.Job.Except != nil),
		})
	}
	return out
}

// suggestion keeps the job on one event. GitLab rejects rules next to
// only/except, so a job using them is told to drop them.
func suggestion(job, rule string, legacy bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", job)
	if legacy {
		b.WriteString("  # replaces only: and except:, which cannot be used with rules:\n")
	}
	fmt.Fprintf(&b, "  rules:\n    - if: %s\n", rule)
	return b.String()
}

// joinAnd joins "a", "b", "c" as "a, b and c". It takes two items or more.
func joinAnd(items []string) string {
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
