package eval

import (
	"strings"

	"github.com/damienaulombard/pipewise/internal/config"
)

// Source is the part of the configuration that decided a verdict.
type Source int

// Sources.
const (
	// SourceRule: an entry of rules: (Reason.Index).
	SourceRule Source = iota
	// SourceNoRule: no entry of rules: matched.
	SourceNoRule
	// SourceOnlyExcept: the job's only and except.
	SourceOnlyExcept
	// SourceDefault: the job has neither rules nor only, so GitLab's default
	// `only: [branches, tags]` applies, unless something removes it.
	SourceDefault
)

func (s Source) String() string {
	switch s {
	case SourceRule:
		return "rule"
	case SourceNoRule:
		return "no-rule"
	case SourceOnlyExcept:
		return "only-except"
	default:
		return "default"
	}
}

// Reason says what decided a job's verdict on one event.
type Reason struct {
	// Workflow is set when workflow: rules decided, before the job's own
	// configuration: the pipeline is not created, or may not be.
	Workflow bool
	Source   Source
	// Index is the 1-based position of the rules: entry, for SourceRule.
	Index int
	// Clause is the deciding part of the configuration as written, such as
	// `if: $CI_COMMIT_TAG, when: manual`.
	Clause string
	// Sure is false when the clause may or may not match. The verdict can
	// still be certain, when both outcomes lead to it. SourceNoRule is
	// always sure: every entry was found not to match.
	Sure bool
	// Unknowns are what pipewise cannot see and the clause depends on:
	// variables, changed files, earlier failures…
	Unknowns []string
}

// ruleClause writes a rules: entry the way it reads in the configuration.
func ruleClause(r config.Rule) string {
	if r.Reference != "" {
		return "!reference " + r.Reference
	}
	var parts []string
	if r.If != "" {
		parts = append(parts, "if: "+r.If)
	}
	if r.Changes {
		parts = append(parts, "changes")
	}
	if r.Exists {
		parts = append(parts, "exists")
	}
	if r.When != "" {
		parts = append(parts, "when: "+r.When)
	}
	return strings.Join(parts, ", ")
}

// filterClause writes only and except the way they read in the configuration.
func filterClause(only, except *config.Filter) string {
	var parts []string
	for _, f := range []struct {
		key    string
		filter *config.Filter
	}{{"only", only}, {"except", except}} {
		if f.filter == nil {
			continue
		}
		var keys []string
		if len(f.filter.Refs) > 0 {
			keys = append(keys, "refs ["+strings.Join(f.filter.Refs, ", ")+"]")
		}
		if len(f.filter.Variables) > 0 {
			keys = append(keys, "variables ["+strings.Join(f.filter.Variables, ", ")+"]")
		}
		if f.filter.Changes {
			keys = append(keys, "changes")
		}
		parts = append(parts, f.key+": "+strings.Join(keys, ", "))
	}
	return strings.Join(parts, "; ")
}
