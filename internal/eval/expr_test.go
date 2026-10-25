package eval

import (
	"errors"
	"strings"
	"testing"
)

// mapEnv defines the listed variables and marks some as unknown; every other
// variable is undefined.
type mapEnv struct {
	vars    map[string]string
	present map[string]bool
	unknown map[string]bool
}

func (e mapEnv) Lookup(name string) (string, VarState) {
	if e.unknown[name] {
		return "", UnknownValue
	}
	if e.present[name] {
		return "", DefinedUnknown
	}
	if v, ok := e.vars[name]; ok {
		return v, Defined
	}
	return "", Undefined
}

var testEnv = mapEnv{
	vars: map[string]string{
		"BRANCH":     "main",
		"SLASHED":    "a/b",
		"SUB":        "ai",
		"EMPTY":      "",
		"TRUE":       "true",
		"MULTI":      "a\nb",
		"PATTERN":    "/^ma/",
		"PATTERN_I":  "/^MA/i",
		"MULTILINE":  "x\n/^ma/",
		"NOTPATTERN": "ain",
	},
	present: map[string]bool{"TAG": true, "OTHER_TAG": true},
	unknown: map[string]bool{"PROJ": true},
}

func TestEval(t *testing.T) {
	tests := []struct {
		expr string
		want Truth
	}{
		// A variable alone: defined and not empty.
		{`$BRANCH`, True},
		{`$EMPTY`, False},
		{`$UNDEF`, False},
		{`$PROJ`, Unknown},

		// Literals alone.
		{`"x"`, True},
		{`""`, False},
		{`'x'`, True},
		{`null`, False},
		{`true`, True},
		{`false`, False},
		{`/ab/`, True},

		// Equality.
		{`$BRANCH == "main"`, True},
		{`$BRANCH == 'main'`, True},
		{`$BRANCH != "main"`, False},
		{`$BRANCH == $BRANCH`, True},
		{`$BRANCH == $EMPTY`, False},
		{`"main" == $BRANCH`, True},
		{`$UNDEF == null`, True},
		{`$UNDEF != null`, False},
		{`$EMPTY == null`, False},
		{`$EMPTY == ""`, True},
		{`$UNDEF == ""`, False},
		{`null == null`, True},
		{`$PROJ == "x"`, Unknown},
		{`"x" == $PROJ`, Unknown},
		{`$PROJ != null`, Unknown},

		// A boolean and a string compare as strings; other kinds never match.
		{`$TRUE == true`, True},
		{`true == $TRUE`, True},
		{`true == "false"`, False},
		{`true == true`, True},
		{`true == false`, False},
		{`true == null`, False},
		{`/ab/ == /ab/`, False},

		// Comparisons associate to the left: ($BRANCH == "main") == "true".
		{`$BRANCH == "main" == "true"`, True},

		// Regular expressions: RE2, substring match unless anchored, flags.
		{`$BRANCH =~ /^ma/`, True},
		{`$BRANCH =~ /ai/`, True},
		{`$BRANCH =~ /^MA/`, False},
		{`$BRANCH =~ /^MA/i`, True},
		{`$BRANCH !~ /^ma/`, False},
		{`$BRANCH !~ /^xy/`, True},
		{`$MULTI =~ /a.b/`, False},
		{`$MULTI =~ /a.b/s`, True},
		{`$BRANCH =~ /ma+/U`, True},
		{`$SLASHED =~ /^a\/b$/`, True},
		{`$UNDEF =~ /^$/`, True}, // GitLab matches an undefined variable as ""
		{`null =~ /^$/`, True},
		{`true =~ /^tr/`, True},
		{`"23" =~ /1234/`, False},

		// A variable on the right is read as a pattern...
		{`$BRANCH =~ $PATTERN`, True},
		{`$BRANCH =~ $PATTERN_I`, True},
		{`$BRANCH =~ $MULTILINE`, True},
		// ...and, when it is not one, as a substring test: left in right.
		{`$BRANCH =~ $NOTPATTERN`, False},
		{`$SUB =~ $NOTPATTERN`, True},
		{`$EMPTY =~ $NOTPATTERN`, True},
		{`"23" =~ "1234"`, True},
		{`$BRANCH =~ "main"`, True},

		// No pattern on the right: never a match.
		{`$BRANCH =~ $UNDEF`, False},
		{`$BRANCH !~ $UNDEF`, True},
		{`$BRANCH =~ null`, False},
		{`$BRANCH =~ false`, False},
		{`$BRANCH !~ false`, True},
		{`$PROJ =~ null`, False},

		// Unknown values on either side.
		{`$PROJ =~ /xy/`, Unknown},
		{`$BRANCH =~ $PROJ`, Unknown},

		// && and || return an operand, with Ruby truthiness: "" is true there,
		// while the final result needs a non-empty value.
		{`$BRANCH && $TRUE`, True},
		{`$BRANCH && $UNDEF`, False},
		{`$UNDEF || $BRANCH`, True},
		{`$UNDEF || $UNDEF`, False},
		{`$EMPTY && "x"`, True},
		{`$EMPTY || "x"`, False},

		// Short-circuits decide without the unknown side; otherwise unknown.
		{`$UNDEF && $PROJ`, False},
		{`$BRANCH || $PROJ`, True},
		{`$PROJ && $BRANCH`, Unknown},
		{`$PROJ || $BRANCH`, Unknown},
		{`$BRANCH && $PROJ`, Unknown},
		{`$UNDEF || $PROJ`, Unknown},

		// && binds tighter than ||; parentheses override both.
		{`$UNDEF && $UNDEF || $BRANCH`, True},
		{`$BRANCH || $UNDEF && $UNDEF`, True},
		{`($BRANCH || $UNDEF) && $UNDEF`, False},
		{`(($BRANCH))`, True},
		{`$BRANCH == "main" && $UNDEF == null`, True},
		{`$BRANCH == "x" || $BRANCH =~ /^m/ && $TRUE`, True},

		// ! is "not present", and binds tighter than comparisons.
		{`!$UNDEF`, True},
		{`!$BRANCH`, False},
		{`!$EMPTY`, True},
		{`!"false"`, False},
		{`!""`, True},
		{`!!$BRANCH`, True},
		{`!$PROJ`, Unknown},
		{`!($BRANCH == "main")`, False},
		{`!$EMPTY == "main"`, False}, // (!$EMPTY) == "main", not !($EMPTY == "main")
		{`$BRANCH && !$UNDEF`, True},
		{`!($BRANCH || $UNDEF)`, False},
		// GitLab's shunting-yard also accepts ! after its operand.
		{`$BRANCH !`, False},

		// A variable that exists with a value pipewise cannot know: present,
		// different from null and "", anything else is unknown.
		{`$TAG`, True},
		{`!$TAG`, False},
		{`$TAG == null`, False},
		{`$TAG != null`, True},
		{`$TAG == ""`, False},
		{`null == $TAG`, False},
		{`$TAG == /ab/`, False},
		{`$TAG == "v1.0.0"`, Unknown},
		{`$TAG == true`, Unknown},
		{`$TAG == $OTHER_TAG`, Unknown},
		{`$TAG =~ /^v/`, Unknown},
		{`$BRANCH =~ $TAG`, Unknown},
		{`$TAG && $BRANCH`, True},
		{`$TAG || $PROJ`, True},

		// Inputs left in the expression have no known value.
		{`$[[ inputs.env ]] == "prod"`, Unknown},

		// Whitespace of any kind between tokens.
		{"\t$BRANCH\n==\r\n'main' ", True},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			e, err := ParseExpr(tt.expr)
			if err != nil {
				t.Fatalf("ParseExpr: %v", err)
			}
			got, err := e.Eval(testEnv)
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if got != tt.want {
				t.Errorf("Eval = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tooMany := "!" + strings.Repeat("$A && ", 49) + "$A" // 100 tokens
	tests := []struct {
		expr string
		pos  int
		msg  string
	}{
		{``, 0, "empty expression"},
		{"  \t", 0, "empty expression"},
		{`()`, 0, "empty expression"},
		{`$A ==`, 3, "== needs an operand on each side"},
		{`== $A`, 0, "== needs an operand on each side"},
		{`!`, 0, "! has no operand"},
		{`$A $B`, 3, "missing operator: this part is not joined to the rest"},
		// The operator joins "x" == $B, leaving $A apart: the second part starts at 6.
		{`$A == "x" $B`, 6, "missing operator: this part is not joined to the rest"},
		{`($A`, 0, "unmatched parenthesis"},
		{`$A)`, 2, "unmatched parenthesis"},
		{`$A # x`, 3, `unexpected "#"`},
		{`$A == é`, 6, `unexpected "é"`},
		{`nullable`, 4, `unexpected "a"`},
		{`trueish`, 0, `unexpected "t"`},
		// GitLab rejects single-character patterns.
		{`$A =~ /./`, 6, `unexpected "/"`},
		{`$A =~ /a[/`, 6, "invalid regular expression /a[/"},
		{tooMany, 297, "too many tokens (GitLab reads at most 99)"},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			_, err := ParseExpr(tt.expr)
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("ParseExpr(%q) error = %v, want a *SyntaxError", tt.expr, err)
			}
			if se.Pos != tt.pos || se.Msg != tt.msg {
				t.Errorf("got %q at %d, want %q at %d", se.Msg, se.Pos, tt.msg, tt.pos)
			}
		})
	}
}

func TestParseTokenLimit(t *testing.T) {
	// 99 tokens is the most GitLab accepts.
	expr := strings.Repeat("$A && ", 49) + "$A"
	if _, err := ParseExpr(expr); err != nil {
		t.Fatalf("99 tokens: %v", err)
	}
}

func TestEvalErrors(t *testing.T) {
	tests := []struct {
		expr string
		msg  string
	}{
		{`$BRANCH =~ true`, "position 8: the right side of =~ must be a regular expression"},
		{`$BRANCH !~ true`, "position 8: the right side of !~ must be a regular expression"},
		{`/ab/ =~ /ab/`, "position 5: the left side of =~ cannot be a regular expression"},
		// Errors come up through every kind of node.
		{`!($BRANCH =~ true)`, "position 10"},
		{`($BRANCH =~ true) == true`, "position 9"},
		{`true == ($BRANCH =~ true)`, "position 17"},
		{`($BRANCH =~ true) && true`, "position 9"},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			e, err := ParseExpr(tt.expr)
			if err != nil {
				t.Fatalf("ParseExpr: %v", err)
			}
			got, err := e.Eval(testEnv)
			if err == nil {
				t.Fatalf("Eval = %v, want an error", got)
			}
			if !strings.Contains(err.Error(), tt.msg) {
				t.Errorf("error %q does not contain %q", err, tt.msg)
			}
			if got != False {
				t.Errorf("Eval with an error = %v, want false", got)
			}
		})
	}
}

func TestStrings(t *testing.T) {
	for truth, want := range map[Truth]string{True: "true", False: "false", Unknown: "unknown"} {
		if got := truth.String(); got != want {
			t.Errorf("Truth(%d).String() = %q, want %q", truth, got, want)
		}
	}
	e, err := ParseExpr(`$A == "x"`)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.String(); got != `$A == "x"` {
		t.Errorf("Expr.String() = %q", got)
	}
	if got := (&SyntaxError{Pos: 4, Msg: "boom"}).Error(); got != "position 4: boom" {
		t.Errorf("SyntaxError.Error() = %q", got)
	}
}
