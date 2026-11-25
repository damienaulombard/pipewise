package eval

import (
	"testing"

	"github.com/damienaulombard/pipewise/internal/actions"
)

func TestFilterPattern(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"main", "main", true},
		{"main", "mainline", false},
		{"main", "Main", false},
		// * matches anything but a slash.
		{"release/*", "release/1.0", true},
		{"release/*", "release/", true},
		{"release/*", "release/1/hotfix", false},
		{"*", "feature/x", false},
		// ** matches anything.
		{"release/**", "release/1/hotfix", true},
		{"**", "feature/x", true},
		{"**-beta", "v1/2-beta", true},
		// ? and + apply to the preceding character.
		{"v1?", "v", true},
		{"v1?", "v1", true},
		{"v1?", "v11", false},
		{"v1+", "v111", true},
		{"v1+", "v", false},
		{"v[0-9]+", "v123", true},
		{"v[0-9]?", "v", true},
		{"*+", "ab", true},
		{"**?", "a/b", true},
		// [] matches one character of a list or a range.
		{"v[12]", "v2", true},
		{"v[12]", "v3", false},
		{"v[0-9a-f]", "vc", true},
		// \ escapes a special character; . is literal.
		{`v\*`, "v*", true},
		{`v\*`, "vx", false},
		{"v1.0", "v1x0", false},
		{"v1.0", "v1.0", true},
		// A ? or + with nothing before it, and a lone [, are literal.
		{"?a", "?a", true},
		{"+a", "+a", true},
		{"a[b", "a[b", true},
		{`a\`, `a\`, true},
		// Non-ASCII characters.
		{"café-*", "café-1", true},
	}
	for _, tt := range tests {
		re, err := filterRegexp(tt.pattern)
		if err != nil {
			t.Errorf("%q: %v", tt.pattern, err)
			continue
		}
		if got := re.MatchString(tt.name); got != tt.want {
			t.Errorf("%q on %q = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
	if _, err := filterRegexp("[z-a]"); err == nil {
		t.Error("[z-a]: no error")
	}
}

func TestWorkflowStarts(t *testing.T) {
	pr := func(t actions.Trigger) actions.Trigger { t.Event = "pull_request"; return t }
	push := func(t actions.Trigger) actions.Trigger { t.Event = "push"; return t }
	type want struct {
		verdict         Verdict
		filter, pattern string
	}
	type three [3]want // merge request, default branch, tag
	runs := want{verdict: Runs}
	skip := want{verdict: Skipped}
	tests := []struct {
		name     string
		triggers []actions.Trigger
		want     three
	}{
		{"no trigger", nil, three{skip, skip, skip}},
		{"other events only", []actions.Trigger{{Event: "workflow_dispatch"}, {Event: "pull_request_target"}}, three{skip, skip, skip}},
		{"pull_request alone", []actions.Trigger{pr(actions.Trigger{})}, three{runs, skip, skip}},
		{"push alone", []actions.Trigger{push(actions.Trigger{})}, three{skip, runs, runs}},
		{"both", []actions.Trigger{pr(actions.Trigger{}), push(actions.Trigger{})}, three{runs, runs, runs}},

		// branches and branches-ignore match the default branch: the target
		// of the pull request, or the pushed branch.
		{"branches match", []actions.Trigger{
			pr(actions.Trigger{Branches: []string{"main"}}), push(actions.Trigger{Branches: []string{"dev", "ma*"}}),
		}, three{
			{Runs, "branches", "main"}, {Runs, "branches", "ma*"}, {Skipped, "branches", ""},
		}},
		{"branches do not match", []actions.Trigger{
			pr(actions.Trigger{Branches: []string{"dev"}}), push(actions.Trigger{Branches: []string{"release/**"}}),
		}, three{
			{Skipped, "branches", ""}, {Skipped, "branches", ""}, {Skipped, "branches", ""},
		}},
		{"branches negated, last match wins", []actions.Trigger{
			pr(actions.Trigger{Branches: []string{"**", "!main"}}), push(actions.Trigger{Branches: []string{"!main", "m*"}}),
		}, three{
			{Skipped, "branches", "!main"}, {Runs, "branches", "m*"}, {Skipped, "branches", ""},
		}},
		{"branches-ignore", []actions.Trigger{
			pr(actions.Trigger{BranchesIgnore: []string{"main"}}), push(actions.Trigger{BranchesIgnore: []string{"dev"}}),
		}, three{
			{Skipped, "branches-ignore", "main"}, {Runs, "", ""}, {Skipped, "branches-ignore", ""},
		}},

		// A push with tag filters only does not start on a branch, and the
		// other way round.
		{"tags only", []actions.Trigger{push(actions.Trigger{Tags: []string{"**"}})}, three{
			skip, {Skipped, "tags", ""}, {Runs, "tags", "**"},
		}},
		{"tags-ignore only", []actions.Trigger{push(actions.Trigger{TagsIgnore: []string{"v*"}})}, three{
			skip, {Skipped, "tags-ignore", ""}, {Maybe, "tags-ignore", ""},
		}},
		{"branches and tags", []actions.Trigger{push(actions.Trigger{Branches: []string{"main"}, Tags: []string{"v*"}})}, three{
			skip, {Runs, "branches", "main"}, {Maybe, "tags", ""},
		}},

		// The tag name is unknown: only a filter that decides for every tag
		// is certain.
		{"tags matching some", []actions.Trigger{push(actions.Trigger{Tags: []string{"v*"}})}, three{
			skip, {Skipped, "tags", ""}, {Maybe, "tags", ""},
		}},
		{"tags all then some excluded", []actions.Trigger{push(actions.Trigger{Tags: []string{"**", "!v*"}})}, three{
			skip, {Skipped, "tags", ""}, {Maybe, "tags", ""},
		}},
		{"tags excluded last", []actions.Trigger{push(actions.Trigger{Tags: []string{"v*", "!**"}})}, three{
			skip, {Skipped, "tags", ""}, {Skipped, "tags", "!**"},
		}},
		{"tags negations only", []actions.Trigger{push(actions.Trigger{Tags: []string{"!v*"}})}, three{
			skip, {Skipped, "tags", ""}, {Skipped, "tags", ""},
		}},
		{"tags-ignore every tag", []actions.Trigger{push(actions.Trigger{TagsIgnore: []string{"**"}})}, three{
			skip, {Skipped, "tags-ignore", ""}, {Skipped, "tags-ignore", "**"},
		}},
		{"tags-ignore excluding every tag last", []actions.Trigger{push(actions.Trigger{TagsIgnore: []string{"v*", "!**"}})}, three{
			skip, {Skipped, "tags-ignore", ""}, {Runs, "", ""},
		}},
		{"tags-ignore negations only", []actions.Trigger{push(actions.Trigger{TagsIgnore: []string{"!v*"}})}, three{
			skip, {Skipped, "tags-ignore", ""}, {Runs, "", ""},
		}},

		// paths: depends on the files of the commit; GitHub does not
		// evaluate it for a tag.
		{"paths", []actions.Trigger{pr(actions.Trigger{Paths: true}), push(actions.Trigger{Paths: true})}, three{
			{Maybe, "paths", ""}, {Maybe, "paths", ""}, runs,
		}},
		{"paths after a branch that matches", []actions.Trigger{pr(actions.Trigger{Branches: []string{"main"}, Paths: true}), push(actions.Trigger{Branches: []string{"main"}, Paths: true})}, three{
			{Maybe, "paths", ""}, {Maybe, "paths", ""}, {Skipped, "branches", ""},
		}},
		{"types after a branch that matches", []actions.Trigger{pr(actions.Trigger{Branches: []string{"main"}, Types: []string{"closed"}})}, three{
			{Maybe, "types", ""}, skip, skip,
		}},
		{"paths after a branch that does not match", []actions.Trigger{push(actions.Trigger{Branches: []string{"dev"}, Paths: true})}, three{
			skip, {Skipped, "branches", ""}, {Skipped, "branches", ""},
		}},

		// types: a pull request workflow without synchronize does not run
		// on every push to the pull request.
		{"types with synchronize", []actions.Trigger{pr(actions.Trigger{Types: []string{"opened", "synchronize"}})}, three{runs, skip, skip}},
		{"types without synchronize", []actions.Trigger{pr(actions.Trigger{Types: []string{"labeled"}})}, three{{Maybe, "types", ""}, skip, skip}},
		{"types on push are ignored", []actions.Trigger{push(actions.Trigger{Types: []string{"labeled"}})}, three{skip, runs, runs}},
		{"pull_request tags are ignored", []actions.Trigger{pr(actions.Trigger{Tags: []string{"v*"}})}, three{runs, skip, skip}},

		// A pattern GitHub would reject decides nothing.
		{"invalid pattern", []actions.Trigger{pr(actions.Trigger{Branches: []string{"[z-a]"}}), push(actions.Trigger{BranchesIgnore: []string{"[z-a]"}})}, three{
			{Maybe, "branches", "[z-a]"}, {Maybe, "branches-ignore", "[z-a]"}, {Skipped, "branches-ignore", ""},
		}},
		{"the first trigger of an event counts", []actions.Trigger{push(actions.Trigger{Branches: []string{"dev"}}), push(actions.Trigger{})}, three{
			skip, {Skipped, "branches", ""}, {Skipped, "branches", ""},
		}},
	}
	for _, tt := range tests {
		for _, ev := range Events {
			got := WorkflowStarts(tt.triggers, ev, "main")
			w := tt.want[ev]
			if got.Verdict != w.verdict || got.Filter != w.filter || got.Pattern != w.pattern {
				t.Errorf("%s on %v = %v %q %q, want %v %q %q", tt.name, ev,
					got.Verdict, got.Filter, got.Pattern, w.verdict, w.filter, w.pattern)
			}
			if (got.Trigger == nil) != (w == skip) {
				t.Errorf("%s on %v: trigger %+v", tt.name, ev, got.Trigger)
			}
			if got.Trigger != nil && got.Trigger != &tt.triggers[triggerIndex(tt.triggers, got.Trigger.Event)] {
				t.Errorf("%s on %v: trigger is not the first %s of on:", tt.name, ev, got.Trigger.Event)
			}
		}
	}
}

func triggerIndex(triggers []actions.Trigger, event string) int {
	for i := range triggers {
		if triggers[i].Event == event {
			return i
		}
	}
	return -1
}
