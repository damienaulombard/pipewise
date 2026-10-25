package eval

import "testing"

func TestContextLookup(t *testing.T) {
	type want struct {
		value string
		state VarState
	}
	tests := []struct {
		event Event
		vars  map[string]want
	}{
		{MergeRequest, map[string]want{
			"CI_PIPELINE_SOURCE":                  {"merge_request_event", Defined},
			"CI_DEFAULT_BRANCH":                   {"trunk", Defined},
			"CI_MERGE_REQUEST_TARGET_BRANCH_NAME": {"trunk", Defined},
			"CI_MERGE_REQUEST_IID":                {"", DefinedUnknown},
			"CI_MERGE_REQUEST_SOURCE_BRANCH_NAME": {"", DefinedUnknown},
			"CI_COMMIT_REF_NAME":                  {"", DefinedUnknown},
			"CI_COMMIT_BRANCH":                    {"", Undefined},
			"CI_COMMIT_TAG":                       {"", Undefined},
			"CI_MERGE_REQUEST_LABELS":             {"", UnknownValue},
			"CI_EXTERNAL_PULL_REQUEST_IID":        {"", Undefined},
			"DEPLOY_ENABLED":                      {"", UnknownValue},
		}},
		{DefaultBranch, map[string]want{
			"CI_PIPELINE_SOURCE":     {"push", Defined},
			"CI_COMMIT_BRANCH":       {"trunk", Defined},
			"CI_COMMIT_REF_NAME":     {"trunk", Defined},
			"CI_DEFAULT_BRANCH":      {"trunk", Defined},
			"CI_COMMIT_TAG":          {"", Undefined},
			"CI_OPEN_MERGE_REQUESTS": {"", Undefined},
			"CI_MERGE_REQUEST_IID":   {"", Undefined},
			"CI_COMMIT_MESSAGE":      {"", UnknownValue},
		}},
		{Tag, map[string]want{
			"CI_PIPELINE_SOURCE":     {"push", Defined},
			"CI_COMMIT_TAG":          {"", DefinedUnknown},
			"CI_COMMIT_REF_NAME":     {"", DefinedUnknown},
			"CI_COMMIT_BRANCH":       {"", Undefined},
			"CI_OPEN_MERGE_REQUESTS": {"", Undefined},
			"CI_MERGE_REQUEST_IID":   {"", Undefined},
			"CI_DEFAULT_BRANCH":      {"trunk", Defined},
		}},
	}
	for _, tt := range tests {
		ctx := NewContext(tt.event, "trunk")
		for name, w := range tt.vars {
			v, state := ctx.Lookup(name)
			if v != w.value || state != w.state {
				t.Errorf("%v: Lookup(%s) = %q, %v; want %q, %v", tt.event, name, v, state, w.value, w.state)
			}
		}
	}
}

func TestEventString(t *testing.T) {
	for ev, want := range map[Event]string{MergeRequest: "merge request", DefaultBranch: "default branch", Tag: "tag"} {
		if got := ev.String(); got != want {
			t.Errorf("Event(%d).String() = %q, want %q", ev, got, want)
		}
	}
	if len(Events) != 3 || Events[0] != MergeRequest || Events[2] != Tag {
		t.Errorf("Events = %v", Events)
	}
}
