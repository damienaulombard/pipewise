package rules

import (
	"fmt"
	"strings"

	"github.com/damienaulombard/pipewise/internal/config"
)

// stagesWithoutNeeds reports, once per pipeline, the jobs that wait for
// whole stages because they declare no `needs:`. Not listed: jobs of the
// first stage used (they wait for nothing), of `.pre` and `.post`, trigger
// jobs, and jobs with `needs:`, even `needs: []`. A pipeline with a single
// stage, not counting `.pre` and `.post`, is not reported.
type stagesWithoutNeeds struct{}

func (stagesWithoutNeeds) ID() string { return "stages-without-needs" }

func (r stagesWithoutNeeds) Check(a *Analysis) []Finding {
	p := a.Pipeline
	order := map[string]int{}
	for i, s := range p.Stages {
		order[s] = i
	}
	// first is the first stage used, not counting .pre and .post. With a
	// single stage, every job is in it and nothing is reported.
	first := -1
	for _, j := range p.Jobs {
		i, known := order[j.Stage]
		if j.Trigger || !known || j.Stage == ".pre" || j.Stage == ".post" {
			continue
		}
		if first < 0 || i < first {
			first = i
		}
	}

	var waiting []*config.Job
	for _, j := range p.Jobs {
		i, known := order[j.Stage]
		if j.Trigger || !known || j.Needs || i <= first || j.Stage == ".post" {
			continue
		}
		waiting = append(waiting, j)
	}
	if len(waiting) == 0 {
		return nil
	}

	names := make([]string, len(waiting))
	var fix strings.Builder
	for i, j := range waiting {
		names[i] = fmt.Sprintf("%q", j.Name)
		needs := "<jobs it uses>"
		if j.Dependencies != nil {
			needs = strings.Join(j.Dependencies, ", ")
		}
		fmt.Fprintf(&fix, "%s:\n  needs: [%s]\n", j.Name, needs)
	}
	msg := fmt.Sprintf("Job %s declares no needs:, so it waits for every job of the earlier stages.", names[0])
	if len(names) > 1 {
		msg = fmt.Sprintf("%d jobs declare no needs:, so each waits for every job of the earlier stages: %s.",
			len(names), joinAnd(names))
	}
	line := p.StagesLine
	if line == 0 {
		line = waiting[0].Line
	}
	return []Finding{{
		Rule:     r.ID(),
		Severity: Info,
		Line:     line,
		Message:  msg,
		Explanation: "Without needs:, a job starts only when every job of the earlier stages has finished, " +
			"even those it does not use: the pipeline lasts as long as the slowest job of each stage, added up. " +
			"With needs:, a job starts as soon as the jobs it lists are done. " +
			"The suggestion takes needs: from dependencies: where the job declares them.",
		Suggestion: fix.String(),
	}}
}
