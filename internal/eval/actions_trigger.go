package eval

import (
	"regexp"
	"slices"
	"strings"

	"github.com/damienaulombard/pipewise/internal/actions"
)

// TriggerResult says whether a GitHub Actions workflow starts on an event,
// and what in its `on:` decided it.
type TriggerResult struct {
	// Verdict is Runs when the workflow starts, Skipped when it does not,
	// Maybe when it depends on what pipewise cannot know.
	Verdict Verdict
	// Trigger is the event of `on:` that applies, nil when `on:` has none:
	// the workflow does not start.
	Trigger *actions.Trigger
	// Filter is the filter that decided (`branches`, `tags-ignore`,
	// `paths`, `types`…), and Pattern the pattern that decided; both are
	// empty when the event alone decided.
	Filter, Pattern string
}

// WorkflowStarts decides whether a workflow with these triggers starts on ev,
// in a repository whose default branch is defaultBranch.
//
// The merge request is a `pull_request` targeting the default branch; the
// default branch and the tag are a `push`. `pull_request_target` is not a
// `pull_request`: counting it could only make pipewise report less. The
// name of the tag is unknown, so a tag filter decides only when it decides
// for every tag.
func WorkflowStarts(triggers []actions.Trigger, ev Event, defaultBranch string) TriggerResult {
	name := "push"
	if ev == MergeRequest {
		name = "pull_request"
	}
	i := slices.IndexFunc(triggers, func(t actions.Trigger) bool { return t.Event == name })
	if i < 0 {
		return TriggerResult{Verdict: Skipped}
	}
	t := &triggers[i]
	var decisions []TriggerResult
	branchFilter := filterName("branches", t.Branches, t.BranchesIgnore)
	tagFilter := filterName("tags", t.Tags, t.TagsIgnore)
	switch {
	case ev == Tag && tagFilter != "":
		decisions = append(decisions,
			unknownName("tags", t.Tags, false),
			unknownName("tags-ignore", t.TagsIgnore, true))
	case ev == Tag && branchFilter != "":
		// A push with branch filters only does not start on a tag.
		decisions = append(decisions, TriggerResult{Verdict: Skipped, Filter: branchFilter})
	case ev == DefaultBranch && branchFilter == "" && tagFilter != "":
		// A push with tag filters only does not start on a branch.
		decisions = append(decisions, TriggerResult{Verdict: Skipped, Filter: tagFilter})
	default:
		decisions = append(decisions,
			knownName("branches", t.Branches, false, defaultBranch),
			knownName("branches-ignore", t.BranchesIgnore, true, defaultBranch))
	}
	// GitHub does not evaluate path filters for a tag.
	if t.Paths && ev != Tag {
		decisions = append(decisions, TriggerResult{Verdict: Maybe, Filter: "paths"})
	}
	if ev == MergeRequest && t.Types != nil && !slices.Contains(t.Types, "synchronize") {
		decisions = append(decisions, TriggerResult{Verdict: Maybe, Filter: "types"})
	}
	res := TriggerResult{Verdict: Runs}
	for _, v := range []Verdict{Skipped, Maybe, Runs} {
		if j := slices.IndexFunc(decisions, func(d TriggerResult) bool { return d.Verdict == v && d.Filter != "" }); j >= 0 {
			res = decisions[j]
			break
		}
	}
	res.Trigger = t
	return res
}

// filterName names the filter set among name and name-ignore, empty when
// neither is: a filter is set when its list is not nil.
func filterName(name string, list, ignore []string) string {
	switch {
	case list != nil:
		return name
	case ignore != nil:
		return name + "-ignore"
	default:
		return ""
	}
}

// knownName applies a filter to a known branch name. Patterns are read in
// order and the last one that matches wins; a pattern starting with `!`
// excludes.
func knownName(filter string, patterns []string, ignore bool, name string) TriggerResult {
	if patterns == nil {
		return TriggerResult{Verdict: Runs}
	}
	matched, pattern := false, ""
	for _, p := range patterns {
		body, negated := strings.CutPrefix(p, "!")
		re, err := filterRegexp(body)
		if err != nil {
			return TriggerResult{Verdict: Maybe, Filter: filter, Pattern: p}
		}
		if re.MatchString(name) {
			matched, pattern = !negated, p
		}
	}
	switch {
	case matched == ignore:
		return TriggerResult{Verdict: Skipped, Filter: filter, Pattern: pattern}
	case ignore:
		// Not ignored: nothing in the filter decided.
		return TriggerResult{Verdict: Runs}
	default:
		return TriggerResult{Verdict: Runs, Filter: filter, Pattern: pattern}
	}
}

// unknownName applies a filter to a tag whose name is unknown: it decides
// only when its last pattern matches every name, or when nothing in it can
// match.
func unknownName(filter string, patterns []string, ignore bool) TriggerResult {
	if patterns == nil {
		return TriggerResult{Verdict: Runs}
	}
	inclusive := slices.ContainsFunc(patterns, func(p string) bool { return !strings.HasPrefix(p, "!") })
	last := patterns[len(patterns)-1]
	body, negated := strings.CutPrefix(last, "!")
	switch {
	case !inclusive && ignore:
		// Nothing is ignored.
		return TriggerResult{Verdict: Runs}
	case !inclusive:
		// Nothing can match.
		return TriggerResult{Verdict: Skipped, Filter: filter}
	case body != "**":
		return TriggerResult{Verdict: Maybe, Filter: filter}
	case negated != ignore:
		// Every tag is excluded, or every tag is ignored.
		return TriggerResult{Verdict: Skipped, Filter: filter, Pattern: last}
	case ignore:
		// No tag is ignored.
		return TriggerResult{Verdict: Runs}
	default:
		// Every tag matches.
		return TriggerResult{Verdict: Runs, Filter: filter, Pattern: last}
	}
}

// filterRegexp compiles a GitHub filter pattern: `*` matches anything but
// `/`, `**` anything, `?` and `+` make the preceding character optional or
// repeated, `[]` matches one character of a list or a range, and `\`
// escapes a special character.
func filterRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	prev := ""
	flush := func(next string) {
		b.WriteString(prev)
		prev = next
	}
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '*' && i+1 < len(runes) && runes[i+1] == '*':
			flush(".*")
			i++
		case r == '*':
			flush("[^/]*")
		case (r == '?' || r == '+') && prev != "":
			b.WriteString("(?:" + prev + ")" + string(r))
			prev = ""
		case r == '[' && slices.Contains(runes[i+1:], ']'):
			end := i + 1 + slices.Index(runes[i+1:], ']')
			flush("[" + string(runes[i+1:end]) + "]")
			i = end
		case r == '\\' && i+1 < len(runes):
			flush(regexp.QuoteMeta(string(runes[i+1])))
			i++
		default:
			flush(regexp.QuoteMeta(string(r)))
		}
	}
	flush("")
	b.WriteString("$")
	return regexp.Compile(b.String())
}
