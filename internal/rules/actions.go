package rules

import (
	"github.com/damienaulombard/pipewise/internal/actions"
	"github.com/damienaulombard/pipewise/internal/config"
)

// KindNotEvaluated says that a GitHub Actions workflow was read but not
// evaluated: its jobs get no verdict until pipewise evaluates `on:` and
// `if:` expressions.
const KindNotEvaluated = "not-evaluated"

// AnalyzeWorkflow parses a GitHub Actions workflow and reports what could
// not be read. Jobs get no verdict yet, so the rules do not run, and one
// finding says so. read finds the local reusable workflows jobs call; nil
// when the repository is unknown.
func AnalyzeWorkflow(data []byte, read actions.ReadFunc) *Outcome {
	w := actions.Parse(data, read)
	out := []Finding{{
		Rule:     KindNotEvaluated,
		Severity: Info,
		Message:  "GitHub Actions workflows are parsed, but not evaluated yet: no job gets a verdict.",
		Explanation: "pipewise does not yet work out on which events each job runs, so it cannot tell " +
			"which jobs run more often than they need to. Only problems reading the workflow are reported.",
	}}
	for _, pb := range w.Problems {
		f := problemFinding(pb)
		if pb.Kind == config.KindParseError {
			f.Explanation = "GitHub would reject this workflow. pipewise keeps reading what it can, " +
				"but anything built on this part is unreliable."
		}
		out = append(out, f)
	}
	sortFindings(out)
	return &Outcome{Findings: out}
}
