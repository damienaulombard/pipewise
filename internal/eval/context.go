package eval

import "strings"

// Event is one of the three pipelines a commit meets on its way to a release.
type Event int

// The events pipewise simulates, in the order a commit meets them.
const (
	// MergeRequest is the merge request pipeline, targeting the default branch.
	MergeRequest Event = iota
	// DefaultBranch is the push of the merged commit to the default branch.
	DefaultBranch
	// Tag is the push of a tag on that commit.
	Tag
)

// Events lists the simulated events in order.
var Events = []Event{MergeRequest, DefaultBranch, Tag}

func (e Event) String() string {
	switch e {
	case MergeRequest:
		return "merge request"
	case DefaultBranch:
		return "default branch"
	default:
		return "tag"
	}
}

// Context is the pipeline of one event: its predefined variables, and what
// only/except refs need to know.
//
// Everything pipewise cannot know stays unknown rather than guessed: the
// name of the tag, the source branch of the merge request, project and
// pipeline variables. A verdict that depends on one of them is "maybe".
type Context struct {
	Event         Event
	DefaultBranch string
	vars          map[string]string
	present       map[string]bool
	undefined     map[string]bool
	// undefinedPrefixes are variable families absent from this pipeline.
	undefinedPrefixes []string
}

// NewContext builds the pipeline of ev for a project whose default branch
// is defaultBranch.
func NewContext(ev Event, defaultBranch string) *Context {
	c := &Context{
		Event:         ev,
		DefaultBranch: defaultBranch,
		vars:          map[string]string{"CI_DEFAULT_BRANCH": defaultBranch},
		present:       map[string]bool{},
		undefined:     map[string]bool{},
		// GitHub pull request pipelines are none of the three events.
		undefinedPrefixes: []string{"CI_EXTERNAL_PULL_REQUEST_"},
	}
	switch ev {
	case MergeRequest:
		c.vars["CI_PIPELINE_SOURCE"] = "merge_request_event"
		c.vars["CI_MERGE_REQUEST_TARGET_BRANCH_NAME"] = defaultBranch
		for _, name := range []string{
			"CI_COMMIT_REF_NAME", "CI_MERGE_REQUEST_ID", "CI_MERGE_REQUEST_IID",
			"CI_MERGE_REQUEST_SOURCE_BRANCH_NAME", "CI_MERGE_REQUEST_EVENT_TYPE",
			"CI_MERGE_REQUEST_TITLE",
		} {
			c.present[name] = true
		}
		c.undefined["CI_COMMIT_BRANCH"] = true
		c.undefined["CI_COMMIT_TAG"] = true
	case DefaultBranch:
		c.vars["CI_PIPELINE_SOURCE"] = "push"
		c.vars["CI_COMMIT_BRANCH"] = defaultBranch
		c.vars["CI_COMMIT_REF_NAME"] = defaultBranch
		c.undefined["CI_COMMIT_TAG"] = true
		c.undefined["CI_OPEN_MERGE_REQUESTS"] = true
		c.undefinedPrefixes = append(c.undefinedPrefixes, "CI_MERGE_REQUEST_")
	case Tag:
		c.vars["CI_PIPELINE_SOURCE"] = "push"
		c.present["CI_COMMIT_TAG"] = true
		c.present["CI_COMMIT_REF_NAME"] = true
		c.undefined["CI_COMMIT_BRANCH"] = true
		c.undefined["CI_OPEN_MERGE_REQUESTS"] = true
		c.undefinedPrefixes = append(c.undefinedPrefixes, "CI_MERGE_REQUEST_")
	}
	return c
}

// Lookup implements Env.
func (c *Context) Lookup(name string) (string, VarState) {
	if v, ok := c.vars[name]; ok {
		return v, Defined
	}
	if c.present[name] {
		return "", DefinedUnknown
	}
	if c.undefined[name] {
		return "", Undefined
	}
	for _, p := range c.undefinedPrefixes {
		if strings.HasPrefix(name, p) {
			return "", Undefined
		}
	}
	return "", UnknownValue
}
