// Package eval decides, for each job, whether it runs on a merge request,
// on the default branch and on a tag, by evaluating its `rules`,
// `only`/`except` and the pipeline's `workflow: rules`.
package eval

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Truth is the outcome of an expression. Unknown means it depends on a
// variable whose value pipewise cannot know, such as a project variable.
type Truth int

// Truth values.
const (
	False Truth = iota
	True
	Unknown
)

func (t Truth) String() string {
	switch t {
	case True:
		return "true"
	case False:
		return "false"
	default:
		return "unknown"
	}
}

// VarState says what is known about a variable in a simulated pipeline.
type VarState int

// Variable states.
const (
	// Undefined: the variable is known not to exist.
	Undefined VarState = iota
	// Defined: the variable exists, with the value returned alongside.
	Defined
	// DefinedUnknown: the variable exists and is not empty, but its value
	// cannot be known, such as the name of the tag being pushed.
	DefinedUnknown
	// UnknownValue: the variable may or may not exist, with any value.
	UnknownValue
)

// Env gives the variables an expression is evaluated against.
type Env interface {
	Lookup(name string) (string, VarState)
}

// SyntaxError is an expression GitLab would reject.
type SyntaxError struct {
	// Pos is the byte offset in the expression.
	Pos int
	Msg string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("position %d: %s", e.Pos, e.Msg)
}

// Expr is a parsed `rules:if` expression.
//
// It follows GitLab's own implementation (lib/gitlab/ci/pipeline/expression)
// rather than a grammar of its own, so that pipewise and GitLab agree on
// every expression, odd ones included:
//
//   - tokens are read in GitLab's order, at most 99 per expression;
//   - operators are ordered by a shunting-yard pass with GitLab's
//     precedences: `!` (1) binds tighter than `==` `!=` `=~` `!~` (10),
//     then `&&` (11), then `||` (12), so `!$A == "x"` is `(!$A) == "x"`;
//   - `&&` and `||` return one of their operands with Ruby truthiness, where
//     only null and false are false, while the final result is true only
//     when it is present: a non-empty string, true or a pattern.
type Expr struct {
	src  string
	root node
}

// ParseExpr parses a `rules:if` expression.
func ParseExpr(src string) (*Expr, error) {
	tokens, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	root, err := build(toRPN(tokens))
	if err != nil {
		return nil, err
	}
	return &Expr{src: src, root: root}, nil
}

func (e *Expr) String() string { return e.src }

// Eval evaluates the expression against env.
func (e *Expr) Eval(env Env) (Truth, error) {
	v, err := e.root.eval(env)
	if err != nil {
		return False, err
	}
	if v.kind == kindUnknown {
		return Unknown, nil
	}
	if v.present() {
		return True, nil
	}
	return False, nil
}

// --- lexing ---------------------------------------------------------------

type lexKind int

const (
	lexValue lexKind = iota
	lexUnary
	lexBinary
	lexOpen
	lexClose
)

type op int

const (
	opEq op = iota + 1
	opNe
	opMatch
	opNotMatch
	opAnd
	opOr
	opNot
)

var opText = map[op]string{
	opEq: "==", opNe: "!=", opMatch: "=~", opNotMatch: "!~",
	opAnd: "&&", opOr: "||", opNot: "!",
}

type valueKind int

const (
	valInput valueKind = iota + 1
	valVariable
	valString
	valPattern
	valNull
	valBoolean
)

type lexeme struct {
	kind  lexKind
	op    op
	value valueKind
	prec  int
	re    *regexp.Regexp
}

// lexemes are tried in GitLab's order; the first that matches wins.
var lexemes = []lexeme{
	{kind: lexOpen, prec: 901, re: regexp.MustCompile(`^\(`)},
	{kind: lexClose, prec: 900, re: regexp.MustCompile(`^\)`)},
	{kind: lexValue, value: valInput, re: regexp.MustCompile(`^\$\[\[\s*inputs\.([\w-]+)\s*\]\]`)},
	{kind: lexValue, value: valVariable, re: regexp.MustCompile(`^\$(\w+)`)},
	{kind: lexValue, value: valString, re: regexp.MustCompile(`^(?:"(.*?)"|'(.*?)')`)},
	{kind: lexValue, value: valPattern, re: regexp.MustCompile(`^/(?:[^/]|\\/)+[^\\]/[ismU]*`)},
	{kind: lexValue, value: valNull, re: regexp.MustCompile(`^null`)},
	{kind: lexValue, value: valBoolean, re: regexp.MustCompile(`^\b(?:true|false)\b`)},
	{kind: lexBinary, op: opEq, prec: 10, re: regexp.MustCompile(`^==`)},
	{kind: lexBinary, op: opMatch, prec: 10, re: regexp.MustCompile(`^=~`)},
	{kind: lexBinary, op: opNe, prec: 10, re: regexp.MustCompile(`^!=`)},
	{kind: lexBinary, op: opNotMatch, prec: 10, re: regexp.MustCompile(`^!~`)},
	{kind: lexBinary, op: opAnd, prec: 11, re: regexp.MustCompile(`^&&`)},
	{kind: lexBinary, op: opOr, prec: 12, re: regexp.MustCompile(`^\|\|`)},
	{kind: lexUnary, op: opNot, prec: 1, re: regexp.MustCompile(`^!`)},
}

var spaces = regexp.MustCompile(`^[ \t\r\n\f\v]+`)

// maxTokens mirrors GitLab's loop, which stops after this many iterations
// even when the expression ends exactly there: 99 tokens is the real limit.
const maxTokens = 100

type token struct {
	lex  *lexeme
	pos  int
	text string
	// group is the first non-empty capture: a variable or input name, or the
	// content of a string.
	group string
}

func tokenize(src string) ([]token, error) {
	var tokens []token
	pos := 0
	for range maxTokens {
		pos += len(spaces.FindString(src[pos:]))
		if pos == len(src) {
			if len(tokens) == 0 {
				return nil, &SyntaxError{Pos: 0, Msg: "empty expression"}
			}
			return tokens, nil
		}
		t, ok := scan(src, pos)
		if !ok {
			return nil, &SyntaxError{Pos: pos, Msg: fmt.Sprintf("unexpected %q", firstRune(src[pos:]))}
		}
		tokens = append(tokens, t)
		pos += len(t.text)
	}
	return nil, &SyntaxError{Pos: pos, Msg: fmt.Sprintf("too many tokens (GitLab reads at most %d)", maxTokens-1)}
}

func scan(src string, pos int) (token, bool) {
	for i := range lexemes {
		m := lexemes[i].re.FindStringSubmatch(src[pos:])
		if m == nil {
			continue
		}
		t := token{lex: &lexemes[i], pos: pos, text: m[0]}
		for _, g := range m[1:] {
			if g != "" {
				t.group = g
				break
			}
		}
		return t, true
	}
	return token{}, false
}

// --- parsing --------------------------------------------------------------

// toRPN is GitLab's shunting-yard pass. An unmatched `)` is kept and
// reported by build; an unclosed `(` ends up in the output, also reported.
func toRPN(tokens []token) []token {
	var out, ops []token
	consume := func(t token) {
		for len(ops) > 0 && t.lex.prec >= ops[len(ops)-1].lex.prec {
			out = append(out, ops[len(ops)-1])
			ops = ops[:len(ops)-1]
		}
	}
	for _, t := range tokens {
		switch t.lex.kind {
		case lexValue:
			out = append(out, t)
		case lexBinary:
			consume(t)
			ops = append(ops, t)
		case lexOpen, lexUnary:
			ops = append(ops, t)
		case lexClose:
			consume(t)
			if len(ops) == 0 {
				out = append(out, t)
				continue
			}
			// Only an opening parenthesis outranks a closing one.
			ops = ops[:len(ops)-1]
		}
	}
	for i := len(ops) - 1; i >= 0; i-- {
		out = append(out, ops[i])
	}
	return out
}

type stackItem struct {
	n   node
	pos int
}

func build(rpn []token) (node, error) {
	var stack []stackItem
	for _, t := range rpn {
		switch t.lex.kind {
		case lexValue:
			n, err := buildValue(t)
			if err != nil {
				return nil, err
			}
			stack = append(stack, stackItem{n: n, pos: t.pos})
		case lexUnary:
			if len(stack) == 0 {
				return nil, &SyntaxError{Pos: t.pos, Msg: "! has no operand"}
			}
			top := stack[len(stack)-1]
			stack[len(stack)-1] = stackItem{n: &notNode{operand: top.n}, pos: t.pos}
		case lexBinary:
			if len(stack) < 2 {
				return nil, &SyntaxError{Pos: t.pos, Msg: fmt.Sprintf("%s needs an operand on each side", opText[t.lex.op])}
			}
			left, right := stack[len(stack)-2], stack[len(stack)-1]
			stack = stack[:len(stack)-2]
			stack = append(stack, stackItem{
				n:   &binaryNode{op: t.lex.op, left: left.n, right: right.n, pos: t.pos},
				pos: left.pos,
			})
		default:
			return nil, &SyntaxError{Pos: t.pos, Msg: "unmatched parenthesis"}
		}
	}
	if len(stack) == 0 {
		return nil, &SyntaxError{Pos: 0, Msg: "empty expression"}
	}
	if len(stack) > 1 {
		return nil, &SyntaxError{Pos: stack[1].pos, Msg: "missing operator: this part is not joined to the rest"}
	}
	return stack[0].n, nil
}

func buildValue(t token) (node, error) {
	switch t.lex.value {
	case valInput:
		return inputNode{}, nil
	case valVariable:
		return variableNode{name: t.group}, nil
	case valString:
		return literalNode{v: value{kind: kindString, s: t.group}}, nil
	case valPattern:
		re, ok := compilePattern(t.text)
		if !ok {
			return nil, &SyntaxError{Pos: t.pos, Msg: fmt.Sprintf("invalid regular expression %s", t.text)}
		}
		return literalNode{v: value{kind: kindPattern, re: re}}, nil
	case valNull:
		return literalNode{v: value{kind: kindNull}}, nil
	default:
		return literalNode{v: value{kind: kindBool, b: t.text == "true"}}, nil
	}
}

var rubyPattern = regexp.MustCompile(`(?m)^/(.*)/([ismU]*)$`)

// compilePattern reads "/regexp/flags" the way GitLab does, escaped slashes
// included, into an RE2 expression.
func compilePattern(s string) (*regexp.Regexp, bool) {
	m := rubyPattern.FindStringSubmatch(strings.ReplaceAll(s, `\/`, `/`))
	if m == nil {
		return nil, false
	}
	expr := m[1]
	if m[2] != "" {
		expr = "(?" + m[2] + ")" + expr
	}
	re, err := regexp.Compile(expr)
	return re, err == nil
}

// --- evaluation -----------------------------------------------------------

type kind int

const (
	kindNull kind = iota
	kindString
	kindBool
	kindPattern
	// kindSome is a non-empty string whose content is unknown.
	kindSome
	kindUnknown
)

type value struct {
	kind kind
	s    string
	b    bool
	re   *regexp.Regexp
}

var unknown = value{kind: kindUnknown}

func boolValue(b bool) value { return value{kind: kindBool, b: b} }

// truthy is Ruby truthiness: everything but null and false.
func (v value) truthy() bool {
	return v.kind != kindNull && (v.kind != kindBool || v.b)
}

// present is ActiveSupport's present?: an empty string is not present.
func (v value) present() bool {
	return v.truthy() && (v.kind != kindString || v.s != "")
}

type node interface {
	eval(env Env) (value, error)
}

type literalNode struct{ v value }

func (n literalNode) eval(Env) (value, error) { return n.v, nil }

type variableNode struct{ name string }

func (n variableNode) eval(env Env) (value, error) {
	s, state := env.Lookup(n.name)
	switch state {
	case Defined:
		return value{kind: kindString, s: s}, nil
	case Undefined:
		return value{kind: kindNull}, nil
	case DefinedUnknown:
		return value{kind: kindSome}, nil
	default:
		return unknown, nil
	}
}

// inputNode is `$[[ inputs.name ]]`: in a merged configuration inputs are
// already interpolated, so one left here has no known value.
type inputNode struct{}

func (inputNode) eval(Env) (value, error) { return unknown, nil }

type notNode struct{ operand node }

func (n *notNode) eval(env Env) (value, error) {
	v, err := n.operand.eval(env)
	if err != nil || v.kind == kindUnknown {
		return v, err
	}
	return boolValue(!v.present()), nil
}

type binaryNode struct {
	op          op
	left, right node
	pos         int
}

func (n *binaryNode) eval(env Env) (value, error) {
	l, err := n.left.eval(env)
	if err != nil {
		return value{}, err
	}
	switch n.op {
	case opAnd, opOr:
		// Short-circuit like Ruby: the right side is evaluated only when
		// the left one does not decide.
		if l.kind == kindUnknown {
			return unknown, nil
		}
		if l.truthy() == (n.op == opOr) {
			return l, nil
		}
		return n.right.eval(env)
	}
	r, err := n.right.eval(env)
	if err != nil {
		return value{}, err
	}
	switch n.op {
	case opEq, opNe:
		eq := equal(l, r)
		if eq == Unknown {
			return unknown, nil
		}
		return boolValue((eq == True) == (n.op == opEq)), nil
	default:
		return n.match(l, r)
	}
}

// match is `=~` and `!~`.
func (n *binaryNode) match(l, r value) (value, error) {
	negate := n.op == opNotMatch
	switch {
	case r.kind == kindUnknown, r.kind == kindSome:
		return unknown, nil
	case !r.truthy():
		// GitLab: no pattern on the right means no match.
		return boolValue(negate), nil
	case r.kind == kindBool:
		return value{}, fmt.Errorf("position %d: the right side of %s must be a regular expression", n.pos, opText[n.op])
	case l.kind == kindUnknown, l.kind == kindSome:
		return unknown, nil
	case l.kind == kindPattern:
		return value{}, fmt.Errorf("position %d: the left side of %s cannot be a regular expression", n.pos, opText[n.op])
	}

	text := l.s
	switch l.kind {
	case kindBool:
		text = fmt.Sprint(l.b)
	case kindNull:
		text = ""
	}

	var matched bool
	if r.kind == kindPattern {
		matched = r.re.MatchString(text)
	} else if re, ok := compilePattern(r.s); ok {
		// A variable on the right is read as "/regexp/flags"...
		matched = re.MatchString(text)
	} else {
		// ...and when it is not one, GitLab falls back to a substring test,
		// left in right.
		matched = strings.Contains(r.s, text)
	}
	return boolValue(matched != negate), nil
}

// equal is GitLab's compare_with_coercion: a boolean and a string compare as
// strings; otherwise values must be of the same kind and equal. Two patterns
// are never equal, being distinct objects. A string of unknown content is
// known to differ only from what no non-empty string can equal.
func equal(l, r value) Truth {
	if l.kind == kindUnknown || r.kind == kindUnknown {
		return Unknown
	}
	if l.kind == kindSome || r.kind == kindSome {
		other := l
		if l.kind == kindSome {
			other = r
		}
		switch {
		case other.kind == kindNull, other.kind == kindPattern,
			other.kind == kindString && other.s == "":
			return False
		default:
			return Unknown
		}
	}
	if l.kind == kindBool && r.kind == kindString {
		return truth(fmt.Sprint(l.b) == r.s)
	}
	if l.kind == kindString && r.kind == kindBool {
		return truth(l.s == fmt.Sprint(r.b))
	}
	if l.kind != r.kind {
		return False
	}
	switch l.kind {
	case kindNull:
		return True
	case kindString:
		return truth(l.s == r.s)
	case kindBool:
		return truth(l.b == r.b)
	default:
		return False
	}
}

func truth(b bool) Truth {
	if b {
		return True
	}
	return False
}

func firstRune(s string) string {
	r, _ := utf8.DecodeRuneInString(s)
	return string(r)
}
