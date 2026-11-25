package eval

// ActionsContext is what a GitHub Actions job-level `if:` can know about one
// of the three simulated events: the `github` context.
//
// The events are a `pull_request` targeting the default branch, a `push` to
// the default branch and a `push` of a tag. Everything pipewise cannot know
// stays unknown rather than guessed: the number of the pull request and its
// source branch, the name of the tag, and every other context (`env`,
// `vars`, `inputs`, `needs`, `secrets`, `matrix`, `steps`…).
type ActionsContext struct {
	Event         Event
	DefaultBranch string
	github        map[string]aval
}

// NewActionsContext builds the `github` context of ev for a repository whose
// default branch is defaultBranch.
func NewActionsContext(ev Event, defaultBranch string) *ActionsContext {
	str := func(s string) aval { return aval{kind: aStr, s: s} }
	// unknown is a field set to a non-empty value pipewise cannot know.
	unknown := func(name string) aval {
		return aval{kind: aUnknown, truthy: True, refs: []string{"github." + name}}
	}
	// prefixed is a field whose value starts with a known prefix and goes
	// on with something pipewise cannot know.
	prefixed := func(name, prefix string) aval {
		return aval{kind: aPrefix, s: prefix, refs: []string{"github." + name}}
	}
	var g map[string]aval
	switch ev {
	case MergeRequest:
		g = map[string]aval{
			"event_name": str("pull_request"),
			"ref":        prefixed("ref", "refs/pull/"),
			"ref_name":   unknown("ref_name"),
			"ref_type":   str("branch"),
			"base_ref":   str(defaultBranch),
			"head_ref":   unknown("head_ref"),
		}
	case DefaultBranch:
		g = map[string]aval{
			"event_name": str("push"),
			"ref":        str("refs/heads/" + defaultBranch),
			"ref_name":   str(defaultBranch),
			"ref_type":   str("branch"),
			"head_ref":   str(""),
		}
	default:
		g = map[string]aval{
			"event_name": str("push"),
			"ref":        prefixed("ref", "refs/tags/"),
			"ref_name":   unknown("ref_name"),
			"ref_type":   str("tag"),
			"head_ref":   str(""),
		}
	}
	// base_ref of a push is left unknown: GitHub documents it for pull
	// requests only, and a tag push can carry the branch it was made on.
	return &ActionsContext{Event: ev, DefaultBranch: defaultBranch, github: g}
}
