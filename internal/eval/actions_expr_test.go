package eval

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// evalActions parses src and evaluates it on ev, for a project whose default
// branch is main.
func evalActions(t *testing.T, src string, ev Event) ActionsResult {
	t.Helper()
	e, err := ParseActionsExpr(src)
	if err != nil {
		t.Fatalf("ParseActionsExpr(%q): %v", src, err)
	}
	return e.Eval(NewActionsContext(ev, "main"))
}

func TestActionsExprLiterals(t *testing.T) {
	tests := []struct {
		expr string
		want Truth
	}{
		{`true`, True},
		{`false`, False},
		{`null`, False},
		{`0`, False},
		{`-0`, False},
		{`1`, True},
		{`-2.5`, True},
		{`1e3`, True},
		{`.5`, True},
		{`0x10`, True},
		{`0x0`, False},
		{`'x'`, True},
		{`''`, False},
		{`'it''s'`, True},
		{`'it''s' == 'IT''S'`, True},
		// The ${{ }} wrapper is optional, as GitHub accepts both.
		{`${{ true }}`, True},
		{"  ${{ false }}  ", False},
		{`${{false}}`, False},
		// No condition is GitHub's default, success().
		{``, True},
		{`${{ }}`, True},
		{`(((true)))`, True},
	}
	for _, tt := range tests {
		for _, ev := range Events {
			got := evalActions(t, tt.expr, ev)
			if got.Truth != tt.want || got.Unknowns != nil {
				t.Errorf("%q on %v = %+v, want %v", tt.expr, ev, got, tt.want)
			}
		}
	}
}

func TestActionsExprOperators(t *testing.T) {
	tests := []struct {
		expr string
		want Truth
	}{
		{`!true`, False},
		{`!false`, True},
		{`!!'x'`, True},
		{`!0`, True},

		// Loose equality: strings ignore case, other types are coerced to
		// numbers.
		{`'abc' == 'ABC'`, True},
		{`'abc' != 'ABC'`, False},
		{`'abc' == 'abd'`, False},
		{`1 == 1.0`, True},
		{`1 == '1'`, True},
		{`'' == 0`, True},
		{`' 2 ' == 2`, True},
		{`'0x1A' == 26`, True},
		{`'0xz' == 0`, False},
		{`'abc' == 0`, False},
		{`null == 0`, True},
		{`null == null`, True},
		{`null == false`, True},
		{`true == 1`, True},
		{`true == true`, True},
		{`true == false`, False},
		{`false == ''`, True},
		{`'true' == true`, False},
		{`0x0 == null`, True},

		// Ordering: strings ignore case, other types are numbers.
		{`1 < 2`, True},
		{`2 < 1`, False},
		{`2 <= 2`, True},
		{`3 > 2`, True},
		{`2 >= 3`, False},
		{`'a' < 'B'`, True},
		{`'b' <= 'A'`, False},
		{`'b' > 'A'`, True},
		{`'a' >= 'A'`, True},
		{`'10' > 9`, True},
		{`'x' < 1`, False},
		{`'x' >= 1`, False},
		{`null < 1`, True},
		{`false < true`, True},

		// && and || return one of their operands.
		{`true && 'x'`, True},
		{`true && ''`, False},
		{`'' && true`, False},
		{`false || 'x'`, True},
		{`'x' || false`, True},
		{`false || 0`, False},
		{`(false || 'a') == 'A'`, True},
		{`(true && 2) == 2`, True},

		// Precedence: ! then ordering then equality then && then ||.
		{`!false == true`, True},
		{`1 < 2 == true`, True},
		{`true || false && false`, True},
		{`(true || false) && false`, False},
		{`false && true || true`, True},
	}
	for _, tt := range tests {
		got := evalActions(t, tt.expr, DefaultBranch)
		if got.Truth != tt.want || got.Unknowns != nil {
			t.Errorf("%q = %+v, want %v", tt.expr, got, tt.want)
		}
	}
}

func TestActionsExprGitHubContext(t *testing.T) {
	type want [3]Truth // merge request, default branch, tag
	tests := []struct {
		expr string
		want want
	}{
		{`github.event_name == 'pull_request'`, want{True, False, False}},
		{`github.event_name == 'push'`, want{False, True, True}},
		{`github.event_name != 'pull_request'`, want{False, True, True}},
		{`GitHub.Event_Name == 'PUSH'`, want{False, True, True}},
		{`github['event_name'] == 'push'`, want{False, True, True}},

		// github.ref: the pull request number and the tag name are unknown,
		// but the kind of ref is not.
		{`github.ref == 'refs/heads/main'`, want{False, True, False}},
		{`github.ref == 'refs/heads/dev'`, want{False, False, False}},
		{`github.ref == 'refs/tags/v1'`, want{False, False, Unknown}},
		{`github.ref == 'refs/tags/'`, want{False, False, False}},
		{`github.ref != 'refs/tags/v1'`, want{True, True, Unknown}},
		{`github.ref == 'refs/pull/1/merge'`, want{Unknown, False, False}},
		{`github.ref == 1`, want{False, False, False}},
		{`github.ref == github.ref`, want{Unknown, True, Unknown}},
		{`github.ref < 'z'`, want{Unknown, True, Unknown}},
		{`github.ref`, want{True, True, True}},
		{`!github.ref`, want{False, False, False}},

		{`github.ref_name == 'main'`, want{Unknown, True, Unknown}},
		{`github.ref_name`, want{True, True, True}},
		{`github.ref_type == 'tag'`, want{False, False, True}},
		{`github.ref_type == 'branch'`, want{True, True, False}},
		{`github.base_ref == 'main'`, want{True, Unknown, Unknown}},
		{`github.head_ref == 'feature'`, want{Unknown, False, False}},
		{`github.head_ref == ''`, want{Unknown, True, True}},
	}
	for _, tt := range tests {
		for _, ev := range Events {
			got := evalActions(t, tt.expr, ev)
			if got.Truth != tt.want[ev] {
				t.Errorf("%q on %v = %+v, want %v", tt.expr, ev, got, tt.want[ev])
			}
			if (got.Truth == Unknown) != (got.Unknowns != nil) {
				t.Errorf("%q on %v: unknowns %v with truth %v", tt.expr, ev, got.Unknowns, got.Truth)
			}
		}
	}
}

func TestActionsExprFunctions(t *testing.T) {
	type want [3]Truth // merge request, default branch, tag
	tests := []struct {
		expr string
		want want
	}{
		{`contains('Hello', 'ELL')`, want{True, True, True}},
		{`contains('Hello', 'x')`, want{False, False, False}},
		{`contains(12, 2)`, want{True, True, True}},
		{`contains(null, '')`, want{True, True, True}},
		{`contains(true, 'ru')`, want{True, True, True}},
		{`contains(false, 'als')`, want{True, True, True}},
		{`contains(github.ref, 'tags')`, want{Unknown, False, True}},
		{`contains(github.ref, 'v1')`, want{Unknown, False, Unknown}},
		{`contains('refs/tags/v1', github.ref)`, want{Unknown, False, Unknown}},

		{`startsWith('Hello', 'he')`, want{True, True, True}},
		{`startsWith('Hello', 'lo')`, want{False, False, False}},
		{`startsWith(github.ref, 'refs/tags/')`, want{False, False, True}},
		{`startsWith(github.ref, 'REFS/')`, want{True, True, True}},
		{`startsWith(github.ref, 'refs/tags/v')`, want{False, False, Unknown}},
		{`startsWith(github.ref, 'refs/heads/')`, want{False, True, False}},
		{`startsWith('refs/tags/v1', github.ref)`, want{Unknown, False, Unknown}},

		{`endsWith('Hello', 'LO')`, want{True, True, True}},
		{`endsWith('Hello', 'he')`, want{False, False, False}},
		{`endsWith(github.ref, '/merge')`, want{Unknown, False, Unknown}},
		{`endsWith(github.ref, github.ref)`, want{Unknown, True, Unknown}},

		{`format('{0}-{1}', 'a', 1) == 'a-1'`, want{True, True, True}},
		{`format('{{0}} {0}', null) == '{0} '`, want{True, True, True}},
		{`format('{0}{0}', true, 2) == 'truetrue'`, want{True, True, True}},
		{`format('{0}', 1.5) == '1.5'`, want{True, True, True}},
		{`format('{0}', false) == 'false'`, want{True, True, True}},
		{`format('x}}y') == 'x}y'`, want{True, True, True}},
		{`format('refs/heads/{0}', 'main') == github.ref`, want{False, True, False}},
		{`format('{0}', github.ref_name) == 'main'`, want{Unknown, True, Unknown}},
		{`format(github.ref_name, 'x') == 'main'`, want{Unknown, True, Unknown}},
		{`format(github.ref, 'x') == 'main'`, want{Unknown, False, Unknown}},
		{`format(github.event_name) == 'push'`, want{False, True, True}},

		// At job level, before anything failed.
		{`success()`, want{True, True, True}},
		{`always()`, want{True, True, True}},
		{`failure()`, want{False, False, False}},
		{`cancelled()`, want{False, False, False}},
		{`SUCCESS()`, want{True, True, True}},
		{`success() && github.event_name == 'push'`, want{False, True, True}},
		{`failure() || github.event_name == 'push'`, want{False, True, True}},

		// Functions pipewise does not evaluate.
		{`fromJSON('true')`, want{Unknown, Unknown, Unknown}},
		{`toJSON(github) != ''`, want{Unknown, Unknown, Unknown}},
		{`join(github.event.commits.*.message) != ''`, want{Unknown, Unknown, Unknown}},
		{`hashFiles('go.sum') != ''`, want{Unknown, Unknown, Unknown}},
	}
	for _, tt := range tests {
		for _, ev := range Events {
			got := evalActions(t, tt.expr, ev)
			if got.Truth != tt.want[ev] {
				t.Errorf("%q on %v = %+v, want %v", tt.expr, ev, got, tt.want[ev])
			}
		}
	}
}

func TestActionsExprUnknowns(t *testing.T) {
	tests := []struct {
		expr string
		ev   Event
		want ActionsResult
	}{
		// Unknown contexts propagate, except where && or || decide alone.
		{`env.DEPLOY == 'true'`, DefaultBranch, ActionsResult{Unknown, []string{"env.deploy"}}},
		{`vars.X`, DefaultBranch, ActionsResult{Unknown, []string{"vars.x"}}},
		{`!inputs.x`, DefaultBranch, ActionsResult{Unknown, []string{"inputs.x"}}},
		{`false && env.X`, DefaultBranch, ActionsResult{False, nil}},
		{`env.X && false`, DefaultBranch, ActionsResult{False, nil}},
		{`!(env.X && false)`, DefaultBranch, ActionsResult{True, nil}},
		{`env.X && true`, DefaultBranch, ActionsResult{Unknown, []string{"env.x"}}},
		{`env.X && env.Y`, DefaultBranch, ActionsResult{Unknown, []string{"env.x", "env.y"}}},
		{`true || env.X`, DefaultBranch, ActionsResult{True, nil}},
		{`env.X || true`, DefaultBranch, ActionsResult{True, nil}},
		{`!(env.X || 'y')`, DefaultBranch, ActionsResult{False, nil}},
		{`env.X || false`, DefaultBranch, ActionsResult{Unknown, []string{"env.x"}}},
		{`(env.X || 'y') == 'y'`, DefaultBranch, ActionsResult{Unknown, []string{"env.x"}}},
		{`github.event_name == 'push' && env.X`, MergeRequest, ActionsResult{False, nil}},
		{`github.event_name == 'push' && env.X`, DefaultBranch, ActionsResult{Unknown, []string{"env.x"}}},

		// Unknowns are listed once, sorted, as written in lower case.
		{`env.B == env.A || env.A`, DefaultBranch, ActionsResult{Unknown, []string{"env.a", "env.b"}}},
		{`github.head_ref == 'x'`, MergeRequest, ActionsResult{Unknown, []string{"github.head_ref"}}},
		{`github.ref == 'refs/tags/v1'`, Tag, ActionsResult{Unknown, []string{"github.ref"}}},
		{`github.repository == 'a/b'`, DefaultBranch, ActionsResult{Unknown, []string{"github.repository"}}},
		{`github`, DefaultBranch, ActionsResult{Unknown, []string{"github"}}},
		{`github.event.pull_request.draft`, MergeRequest, ActionsResult{Unknown, []string{"github.event.pull_request.draft"}}},
		{`needs.build.outputs['deploy'] == 'yes'`, DefaultBranch, ActionsResult{Unknown, []string{"needs.build.outputs.deploy"}}},
		{`contains(github.event.pull_request.labels.*.name, 'ci')`, MergeRequest, ActionsResult{Unknown, []string{"github.event.pull_request.labels.*.name"}}},
		{`matrix[env.K]`, DefaultBranch, ActionsResult{Unknown, []string{"env.k", "matrix"}}},
		{`github[github.event_name]`, DefaultBranch, ActionsResult{Unknown, []string{"github.push"}}},
		{`github.event.commits[*].id`, DefaultBranch, ActionsResult{Unknown, []string{"github.event.commits.*.id"}}},
		{`github[1]`, DefaultBranch, ActionsResult{Unknown, []string{"github.1"}}},
		{`github['ref_type'] == 'branch'`, DefaultBranch, ActionsResult{True, nil}},
		{`fromJSON(env.X).a`, DefaultBranch, ActionsResult{Unknown, []string{"env.x", "fromJSON()"}}},
		{`fromJSON('{}')`, DefaultBranch, ActionsResult{Unknown, []string{"fromJSON()"}}},
		{`contains(env.X, 'a')`, DefaultBranch, ActionsResult{Unknown, []string{"env.x"}}},
		{`format('{0}', env.X) == 'a'`, DefaultBranch, ActionsResult{Unknown, []string{"env.x"}}},
		{`format(env.F) == 'a'`, DefaultBranch, ActionsResult{Unknown, []string{"env.f"}}},
		{`env.X < 1`, DefaultBranch, ActionsResult{Unknown, []string{"env.x"}}},

		// A property of something that is not an object is null.
		{`('x').y`, DefaultBranch, ActionsResult{False, nil}},
		{`success().y == null`, DefaultBranch, ActionsResult{True, nil}},
		{`('x')[0]`, DefaultBranch, ActionsResult{False, nil}},
		{`github.ref.x`, Tag, ActionsResult{False, nil}},

		// A format string that is not valid is an error at run time:
		// pipewise does not guess what GitHub would do.
		{`format(github.event_name, 'x') == 'x'`, DefaultBranch, ActionsResult{False, nil}},
		{`format(format('{0}', '{1}'), 'x') == 'x'`, DefaultBranch, ActionsResult{Unknown, []string{"format()"}}},
	}
	for _, tt := range tests {
		got := evalActions(t, tt.expr, tt.ev)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%q on %v = %+v, want %+v", tt.expr, tt.ev, got, tt.want)
		}
	}
}

func TestActionsExprSyntaxErrors(t *testing.T) {
	tests := []struct {
		expr string
		pos  int
	}{
		{`'abc`, 0},
		{`"abc"`, 0},
		{`a = b`, 2},
		{`a & b`, 2},
		{`a | b`, 2},
		{`$X`, 0},
		{`${{ a }} && ${{ b }}`, 6},
		{"  ${{ a = b }}", 8},
		{`github[]`, 7},
		{`()`, 1},
		{`== 1`, 0},
		{`github.event_name == `, 21},
		{`(true`, 5},
		{`true)`, 4},
		{`true false`, 5},
		{`!`, 1},
		{`1 ==`, 4},
		{`github.`, 7},
		{`github.'x'`, 7},
		{`github[*`, 8},
		{`github['x'`, 10},
		{`contains('a')`, 0},
		{`contains('a', 'b', 'c')`, 0},
		{`success(1)`, 0},
		{`format()`, 0},
		{`fromJSON()`, 0},
		{`join()`, 0},
		{`join('a', 'b', 'c')`, 0},
		{`hashFiles()`, 0},
		{`nosuch('a')`, 0},
		{`contains('a' 'b')`, 13},
		{`contains('a',`, 13},
		{`format('{0')`, 7},
		{`format('{')`, 7},
		{`format('}')`, 7},
		{`format('{x}')`, 7},
		{`format('{1}', 'a')`, 7},
		{`*`, 0},
		{`1.2.3`, 3},
	}
	for _, tt := range tests {
		_, err := ParseActionsExpr(tt.expr)
		var se *SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%q: error %v, want a SyntaxError", tt.expr, err)
			continue
		}
		if se.Pos != tt.pos {
			t.Errorf("%q: error at %d (%v), want %d", tt.expr, se.Pos, se, tt.pos)
		}
	}
}

// A condition nested deeper than GitHub would ever need is refused, so that
// a hostile workflow cannot exhaust the stack.
func TestActionsExprDepthLimit(t *testing.T) {
	deep := []string{
		strings.Repeat("(", maxActionsDepth+1) + "true" + strings.Repeat(")", maxActionsDepth+1),
		strings.Repeat("!", maxActionsDepth+1) + "true",
		"github" + strings.Repeat("[github", maxActionsDepth+1),
	}
	for _, src := range deep {
		_, err := ParseActionsExpr(src)
		var se *SyntaxError
		if !errors.As(err, &se) || !strings.Contains(se.Msg, "nested") {
			t.Errorf("%.20q…: error %v, want a nesting SyntaxError", src, err)
		}
	}
	ok := strings.Repeat("(", maxActionsDepth-1) + "true" + strings.Repeat(")", maxActionsDepth-1)
	if _, err := ParseActionsExpr(ok); err != nil {
		t.Errorf("%d levels: %v", maxActionsDepth-1, err)
	}
}

func TestActionsExprString(t *testing.T) {
	const src = "${{ github.event_name == 'push' }}"
	e, err := ParseActionsExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	if e.String() != src {
		t.Errorf("String() = %q, want %q", e.String(), src)
	}
}

func TestActionsContextEvent(t *testing.T) {
	c := NewActionsContext(Tag, "trunk")
	if c.Event != Tag || c.DefaultBranch != "trunk" {
		t.Errorf("context = %+v", c)
	}
	got := (&ActionsExpr{root: mustParseActions(t, `github.ref == 'refs/heads/trunk'`)}).Eval(NewActionsContext(DefaultBranch, "trunk"))
	if got.Truth != True {
		t.Errorf("default branch trunk: %+v", got)
	}
}

func mustParseActions(t *testing.T, src string) anode {
	t.Helper()
	e, err := ParseActionsExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	return e.root
}
