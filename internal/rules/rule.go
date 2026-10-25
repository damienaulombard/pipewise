// Package rules turns what internal/config read and internal/eval decided
// into findings a developer can act on, one file per rule.
package rules

import (
	"sort"

	"github.com/damienaulombard/pipewise/internal/config"
	"github.com/damienaulombard/pipewise/internal/eval"
)

// Severity of a finding, ordered: Error is the most severe.
type Severity int

// Severities.
const (
	Info Severity = iota
	Warning
	Error
)

func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	default:
		return "info"
	}
}

// Finding is one thing to fix. Rule ids are part of pipewise's public
// interface: they stay stable.
type Finding struct {
	Rule     string
	Severity Severity
	// Job is empty for a finding that is not about one job.
	Job  string
	Line int
	// Message states the fact, in one sentence.
	Message string
	// Explanation says why it matters, for someone who has not read the
	// doctrine behind the rule.
	Explanation string
	// Suggestion is a YAML snippet that fixes it; empty when there is none.
	Suggestion string
}

// Analysis is what rules look at.
type Analysis struct {
	Pipeline *config.Pipeline
	Result   *eval.Result
}

// Rule checks an analysis.
type Rule interface {
	ID() string
	Check(a *Analysis) []Finding
}

// All returns every rule, in a stable order.
func All() []Rule {
	return []Rule{redundantJob{}, noRules{}, missingInterruptible{}, floatingImage{}, artifactsNoExpiry{}}
}

// Analyze parses a configuration, evaluates it and runs every rule. Problems
// met while reading or evaluating it come back as findings too, under their
// own ids. Findings are sorted by line, then rule id, then job.
func Analyze(data []byte, opts eval.Options) []Finding {
	p := config.Parse(data)
	res := eval.Evaluate(p, opts)
	a := &Analysis{Pipeline: p, Result: res}

	var out []Finding
	for _, pb := range append(append([]config.Problem{}, p.Problems...), res.Problems...) {
		out = append(out, problemFinding(pb))
	}
	for _, r := range All() {
		out = append(out, r.Check(a)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		return out[i].Job < out[j].Job
	})
	return out
}

func problemFinding(pb config.Problem) Finding {
	f := Finding{Rule: pb.Kind, Severity: Warning, Job: pb.Job, Line: pb.Line, Message: pb.Message}
	switch pb.Kind {
	case config.KindParseError:
		f.Severity = Error
		f.Explanation = "GitLab would reject this configuration. pipewise keeps reading what it can, " +
			"but verdicts that depend on this part are unreliable."
	case config.KindUnresolvedInclude:
		f.Explanation = "Jobs from included files are not analysed. " +
			"Analyse the merged configuration instead, shown in the Full configuration tab of GitLab's pipeline editor."
	default:
		f.Explanation = "pipewise cannot tell what this part stands for, " +
			"so the jobs that depend on it are reported as \"maybe\"."
	}
	return f
}
