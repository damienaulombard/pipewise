package rules

import "fmt"

// artifactsNoExpiry reports artifacts without expire_in. Artifacts inherited
// from `default:` are reported once, on `default:`, naming the jobs that
// inherit them.
type artifactsNoExpiry struct{}

func (artifactsNoExpiry) ID() string { return "artifacts-no-expiry" }

const artifactsExplanation = "Artifacts without expire_in are kept as long as the instance default allows: " +
	"30 days on GitLab.com, possibly forever on a self-managed instance. " +
	"Most job artifacts are only needed by the next stages of the same pipeline."

func (r artifactsNoExpiry) Check(a *Analysis) []Finding {
	var out []Finding
	d := a.Pipeline.Defaults
	if d.Artifacts != nil && d.Artifacts.ExpireIn == "" {
		var users []string
		for _, j := range a.Pipeline.Jobs {
			if !j.Trigger && j.Artifacts != nil && j.Artifacts.Inherited {
				users = append(users, fmt.Sprintf("%q", j.Name))
			}
		}
		if len(users) > 0 {
			out = append(out, Finding{
				Rule:        r.ID(),
				Severity:    Info,
				Line:        d.ArtifactsLine,
				Message:     fmt.Sprintf("The default artifacts have no expire_in; %s.", usedBy(users, "them")),
				Explanation: artifactsExplanation,
				Suggestion:  "default:\n  artifacts:\n    expire_in: 1 week\n",
			})
		}
	}
	for _, j := range a.Pipeline.Jobs {
		if j.Trigger || j.Artifacts == nil || j.Artifacts.Inherited || j.Artifacts.ExpireIn != "" {
			continue
		}
		out = append(out, Finding{
			Rule:        r.ID(),
			Severity:    Info,
			Job:         j.Name,
			Line:        j.Line,
			Message:     fmt.Sprintf("Job %q keeps its artifacts with no expire_in.", j.Name),
			Explanation: artifactsExplanation,
			Suggestion:  fmt.Sprintf("%s:\n  artifacts:\n    expire_in: 1 week\n", j.Name),
		})
	}
	return out
}
