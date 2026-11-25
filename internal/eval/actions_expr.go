package eval

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ActionsExpr is a parsed GitHub Actions expression: a job-level `if:`, with
// or without its `${{ }}` wrapper.
//
// It follows GitHub's documented semantics: literals `true`, `false`,
// `null`, numbers and 'strings'; operators `!`, `<` `<=` `>` `>=`, `==`
// `!=`, `&&`, `||` in that order of precedence; loose equality, where
// strings compare ignoring case and values of different types are compared
// as numbers; `&&` and `||` returning one of their operands.
type ActionsExpr struct {
	src  string
	root anode
}

// ActionsResult is the outcome of an expression on one event.
type ActionsResult struct {
	Truth Truth
	// Unknowns are the references that made the result unknown, sorted,
	// in lower case (`env.deploy`, `github.head_ref`, `fromJSON()`). Nil
	// when the result is known.
	Unknowns []string
}

// ParseActionsExpr parses a job-level `if:`. An empty condition is GitHub's
// default, `success()`.
func ParseActionsExpr(src string) (*ActionsExpr, error) {
	inner, offset := src, 0
	trimmed := strings.TrimSpace(src)
	if strings.HasPrefix(trimmed, "${{") && strings.HasSuffix(trimmed, "}}") {
		offset = strings.Index(src, "${{") + 3
		inner = trimmed[3 : len(trimmed)-2]
	}
	if strings.TrimSpace(inner) == "" {
		return &ActionsExpr{src: src, root: aliteral{v: aval{kind: aBool, b: true}}}, nil
	}
	p := &aparser{src: inner}
	root, err := p.parse()
	if err != nil {
		err.Pos += offset
		return nil, err
	}
	return &ActionsExpr{src: src, root: root}, nil
}

func (e *ActionsExpr) String() string { return e.src }

// Eval evaluates the expression on the event of c.
func (e *ActionsExpr) Eval(c *ActionsContext) ActionsResult {
	v := e.root.eval(c)
	t := v.truth()
	if t != Unknown {
		return ActionsResult{Truth: t}
	}
	refs := slices.Clone(v.refs)
	slices.Sort(refs)
	return ActionsResult{Truth: Unknown, Unknowns: slices.Compact(refs)}
}

// --- values ---------------------------------------------------------------

type akind int

const (
	aNull akind = iota
	aBool
	aNum
	aStr
	// aPrefix is a string known to start with s and to go on with
	// something pipewise cannot know, such as `refs/tags/` and a tag name.
	aPrefix
	// aUnknown is a value pipewise cannot know; truthy may still be known.
	aUnknown
)

type aval struct {
	kind   akind
	b      bool
	n      float64
	s      string
	truthy Truth
	// refs are the references the value depends on and does not know.
	refs []string
}

func unknownOf(truthy Truth, vals ...aval) aval {
	var refs []string
	for _, v := range vals {
		refs = append(refs, v.refs...)
	}
	return aval{kind: aUnknown, truthy: truthy, refs: refs}
}

func boolVal(b bool) aval { return aval{kind: aBool, b: b} }

func truthVal(t Truth, vals ...aval) aval {
	if t == Unknown {
		return unknownOf(Unknown, vals...)
	}
	return boolVal(t == True)
}

func (v aval) truth() Truth {
	switch v.kind {
	case aNull:
		return False
	case aBool:
		return truthOf(v.b)
	case aNum:
		return truthOf(v.n != 0 && !math.IsNaN(v.n))
	case aStr:
		return truthOf(v.s != "")
	case aPrefix:
		return True
	default:
		return v.truthy
	}
}

func truthOf(b bool) Truth {
	if b {
		return True
	}
	return False
}

var numberString = regexp.MustCompile(`^-?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?$`)

// num converts a known value the way GitHub does when types differ.
func (v aval) num() float64 {
	switch v.kind {
	case aNull:
		return 0
	case aBool:
		if v.b {
			return 1
		}
		return 0
	case aNum:
		return v.n
	default:
		s := strings.TrimSpace(v.s)
		if s == "" {
			return 0
		}
		return parseNumber(s)
	}
}

func parseNumber(s string) float64 {
	if h, ok := strings.CutPrefix(s, "0x"); ok {
		n, err := strconv.ParseUint(h, 16, 64)
		if err != nil {
			return math.NaN()
		}
		return float64(n)
	}
	if !numberString.MatchString(s) {
		return math.NaN()
	}
	n, _ := strconv.ParseFloat(s, 64)
	return n
}

// str converts a known value to a string, as GitHub's functions do.
func (v aval) str() string {
	switch v.kind {
	case aNull:
		return ""
	case aBool:
		return strconv.FormatBool(v.b)
	case aNum:
		return strconv.FormatFloat(v.n, 'f', -1, 64)
	default:
		return v.s
	}
}

func actionsEqual(a, b aval) Truth {
	if a.kind == aUnknown || b.kind == aUnknown {
		return Unknown
	}
	if a.kind == aPrefix && b.kind == aPrefix {
		return Unknown
	}
	if b.kind == aPrefix {
		a, b = b, a
	}
	if a.kind == aPrefix {
		if b.kind != aStr {
			return False
		}
		s, p := strings.ToLower(b.s), strings.ToLower(a.s)
		if strings.HasPrefix(s, p) && len(s) > len(p) {
			return Unknown
		}
		return False
	}
	if a.kind == aStr && b.kind == aStr {
		return truthOf(strings.EqualFold(a.s, b.s))
	}
	return truthOf(a.num() == b.num())
}

func actionsOrder(a, b aval, op string) Truth {
	if a.kind >= aPrefix || b.kind >= aPrefix {
		return Unknown
	}
	var c int
	if a.kind == aStr && b.kind == aStr {
		c = strings.Compare(strings.ToLower(a.s), strings.ToLower(b.s))
	} else {
		x, y := a.num(), b.num()
		if math.IsNaN(x) || math.IsNaN(y) {
			return False
		}
		c = compareFloat(x, y)
	}
	switch op {
	case "<":
		return truthOf(c < 0)
	case "<=":
		return truthOf(c <= 0)
	case ">":
		return truthOf(c > 0)
	default:
		return truthOf(c >= 0)
	}
}

func compareFloat(x, y float64) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	default:
		return 0
	}
}

// --- nodes ----------------------------------------------------------------

type anode interface {
	eval(c *ActionsContext) aval
}

type aliteral struct{ v aval }

func (n aliteral) eval(*ActionsContext) aval { return n.v }

type anot struct{ x anode }

func (n anot) eval(c *ActionsContext) aval {
	v := n.x.eval(c)
	switch t := v.truth(); t {
	case True, False:
		if v.kind == aUnknown {
			return unknownOf(truthOf(t == False), v)
		}
		return boolVal(t == False)
	default:
		return unknownOf(Unknown, v)
	}
}

type abinary struct {
	op   string
	l, r anode
}

func (n abinary) eval(c *ActionsContext) aval {
	l := n.l.eval(c)
	switch n.op {
	case "&&":
		switch l.truth() {
		case False:
			return l
		case True:
			return n.r.eval(c)
		}
		r := n.r.eval(c)
		if r.truth() == False {
			return unknownOf(False, l, r)
		}
		return unknownOf(Unknown, l, r)
	case "||":
		switch l.truth() {
		case True:
			return l
		case False:
			return n.r.eval(c)
		}
		r := n.r.eval(c)
		if r.truth() == True {
			return unknownOf(True, l, r)
		}
		return unknownOf(Unknown, l, r)
	}
	r := n.r.eval(c)
	switch n.op {
	case "==":
		return truthVal(actionsEqual(l, r), l, r)
	case "!=":
		t := actionsEqual(l, r)
		if t != Unknown {
			t = truthOf(t == False)
		}
		return truthVal(t, l, r)
	default:
		return truthVal(actionsOrder(l, r, n.op), l, r)
	}
}

// asegment is one step of a property access: a name, `*`, or an index
// computed by an expression.
type asegment struct {
	name  string
	star  bool
	index anode
}

// aref is a named value followed by property accesses: `github.ref`,
// `needs.build.outputs['x']`, `github.event.commits.*.message`.
type aref struct {
	root string
	path []asegment
}

func (n aref) eval(c *ActionsContext) aval {
	ref := n.root
	names := make([]string, 0, len(n.path))
	for _, s := range n.path {
		switch {
		case s.star:
			names = append(names, "*")
		case s.index != nil:
			k := s.index.eval(c)
			if k.kind >= aPrefix {
				return unknownOf(Unknown, k, aval{refs: []string{renderRef(ref, names)}})
			}
			names = append(names, strings.ToLower(k.str()))
		default:
			names = append(names, s.name)
		}
	}
	if ref == "github" && len(names) > 0 {
		if v, ok := c.github[names[0]]; ok {
			if len(names) > 1 {
				return aval{kind: aNull}
			}
			return v
		}
	}
	return unknownOf(Unknown, aval{refs: []string{renderRef(ref, names)}})
}

func renderRef(root string, names []string) string {
	if len(names) == 0 {
		return root
	}
	return root + "." + strings.Join(names, ".")
}

// aaccess is a property access on something that is not a named value:
// `fromJSON(x).y`, `('x').y`.
type aaccess struct{ x anode }

func (n aaccess) eval(c *ActionsContext) aval {
	v := n.x.eval(c)
	if v.kind == aUnknown {
		return unknownOf(Unknown, v)
	}
	return aval{kind: aNull}
}

type acall struct {
	name string
	args []anode
}

func (n acall) eval(c *ActionsContext) aval {
	args := make([]aval, len(n.args))
	for i, a := range n.args {
		args[i] = a.eval(c)
	}
	switch n.name {
	case "success", "always":
		return boolVal(true)
	case "failure", "cancelled":
		return boolVal(false)
	case "contains", "startswith", "endswith":
		return truthVal(searchFunc(n.name, args[0], args[1]), args...)
	case "format":
		for _, a := range args {
			if a.kind >= aPrefix {
				return unknownOf(Unknown, args...)
			}
		}
		strs := make([]string, len(args)-1)
		for i, a := range args[1:] {
			strs[i] = a.str()
		}
		s, err := formatString(args[0].str(), strs)
		if err != nil {
			return unknownOf(Unknown, aval{refs: []string{"format()"}})
		}
		return aval{kind: aStr, s: s}
	default:
		return unknownOf(Unknown, append(args, aval{refs: []string{functions[n.name].display + "()"}})...)
	}
}

// searchFunc evaluates contains, startsWith and endsWith on strings,
// ignoring case. A string of which only the start is known can still give a
// known answer.
func searchFunc(name string, hay, needle aval) Truth {
	if hay.kind == aUnknown || needle.kind == aUnknown || needle.kind == aPrefix {
		return Unknown
	}
	h, s := strings.ToLower(hay.str()), strings.ToLower(needle.str())
	if hay.kind == aPrefix {
		switch name {
		case "contains":
			if strings.Contains(h, s) {
				return True
			}
		case "startswith":
			if strings.HasPrefix(h, s) {
				return True
			}
			if !strings.HasPrefix(s, h) {
				return False
			}
		}
		return Unknown
	}
	switch name {
	case "contains":
		return truthOf(strings.Contains(h, s))
	case "startswith":
		return truthOf(strings.HasPrefix(h, s))
	default:
		return truthOf(strings.HasSuffix(h, s))
	}
}

// formatString replaces `{N}` with args[N]; `{{` and `}}` are literal
// braces.
func formatString(f string, args []string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		ch := f[i]
		switch {
		case ch == '{' && strings.HasPrefix(f[i:], "{{"), ch == '}' && strings.HasPrefix(f[i:], "}}"):
			b.WriteByte(ch)
			i++
		case ch == '{':
			end := strings.IndexByte(f[i:], '}')
			if end < 0 {
				return "", fmt.Errorf("unclosed { at %d", i)
			}
			k, err := strconv.Atoi(f[i+1 : i+end])
			if err != nil || k < 0 || k >= len(args) {
				return "", fmt.Errorf("invalid argument %q", f[i:i+end+1])
			}
			b.WriteString(args[k])
			i += end
		case ch == '}':
			return "", fmt.Errorf("unmatched } at %d", i)
		default:
			b.WriteByte(ch)
		}
	}
	return b.String(), nil
}

// functions are the functions GitHub accepts, with their arity; max -1 is
// any number. The ones pipewise does not evaluate give unknown.
var functions = map[string]struct {
	display  string
	min, max int
}{
	"contains":   {"contains", 2, 2},
	"startswith": {"startsWith", 2, 2},
	"endswith":   {"endsWith", 2, 2},
	"format":     {"format", 1, -1},
	"success":    {"success", 0, 0},
	"always":     {"always", 0, 0},
	"failure":    {"failure", 0, 0},
	"cancelled":  {"cancelled", 0, 0},
	"tojson":     {"toJSON", 1, 1},
	"fromjson":   {"fromJSON", 1, 1},
	"join":       {"join", 1, 2},
	"hashfiles":  {"hashFiles", 1, -1},
}

// --- lexing ---------------------------------------------------------------

type atokKind int

const (
	atEOF atokKind = iota
	atNum
	atStr
	atIdent
	atOp
	atPunct
)

type atok struct {
	kind atokKind
	text string
	pos  int
	// n is the value of a number, s the content of a string.
	n float64
	s string
}

var (
	aNumber = regexp.MustCompile(`^(?:0x[0-9a-fA-F]+|-?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?)`)
	aIdent  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*`)
	aOps    = []string{"==", "!=", "<=", ">=", "&&", "||", "<", ">", "!"}
)

func alex(src string) ([]atok, *SyntaxError) {
	var toks []atok
	i := 0
	for {
		for i < len(src) && strings.ContainsRune(" \t\r\n", rune(src[i])) {
			i++
		}
		if i == len(src) {
			return append(toks, atok{kind: atEOF, pos: i}), nil
		}
		t, err := alexOne(src, i)
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		i += len(t.text)
	}
}

func alexOne(src string, i int) (atok, *SyntaxError) {
	rest := src[i:]
	if m := aNumber.FindString(rest); m != "" {
		return atok{kind: atNum, text: m, pos: i, n: parseNumber(m)}, nil
	}
	if m := aIdent.FindString(rest); m != "" {
		return atok{kind: atIdent, text: m, pos: i}, nil
	}
	if rest[0] == '\'' {
		var b strings.Builder
		for j := 1; j < len(rest); j++ {
			if rest[j] != '\'' {
				b.WriteByte(rest[j])
				continue
			}
			if j+1 < len(rest) && rest[j+1] == '\'' {
				b.WriteByte('\'')
				j++
				continue
			}
			return atok{kind: atStr, text: rest[:j+1], pos: i, s: b.String()}, nil
		}
		return atok{}, &SyntaxError{Pos: i, Msg: "unterminated string"}
	}
	for _, op := range aOps {
		if strings.HasPrefix(rest, op) {
			return atok{kind: atOp, text: op, pos: i}, nil
		}
	}
	if strings.ContainsRune("()[],.*", rune(rest[0])) {
		return atok{kind: atPunct, text: rest[:1], pos: i}, nil
	}
	return atok{}, &SyntaxError{Pos: i, Msg: fmt.Sprintf("unexpected %q", firstRune(rest))}
}

// --- parsing --------------------------------------------------------------

// maxActionsDepth bounds the nesting of a condition: no real `if:` comes
// near it, and a deeper one would only exhaust the stack.
const maxActionsDepth = 100

type aparser struct {
	src  string
	toks []atok
	i    int
	// depth counts the operands being parsed inside one another.
	depth int
}

func (p *aparser) parse() (anode, *SyntaxError) {
	toks, err := alex(p.src)
	if err != nil {
		return nil, err
	}
	p.toks = toks
	n, err := p.binary(0)
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != atEOF {
		return nil, unexpected(t)
	}
	return n, nil
}

func (p *aparser) peek() atok { return p.toks[p.i] }

func (p *aparser) next() atok {
	t := p.toks[p.i]
	if t.kind != atEOF {
		p.i++
	}
	return t
}

func (p *aparser) accept(kind atokKind, text string) bool {
	if t := p.peek(); t.kind == kind && t.text == text {
		p.i++
		return true
	}
	return false
}

func (p *aparser) expect(text string) *SyntaxError {
	if !p.accept(atPunct, text) {
		return &SyntaxError{Pos: p.peek().pos, Msg: fmt.Sprintf("expected %q", text)}
	}
	return nil
}

func unexpected(t atok) *SyntaxError {
	if t.kind == atEOF {
		return &SyntaxError{Pos: t.pos, Msg: "unexpected end of expression"}
	}
	return &SyntaxError{Pos: t.pos, Msg: fmt.Sprintf("unexpected %q", t.text)}
}

// levels are the binary operators, from the loosest to the tightest.
var levels = [][]string{{"||"}, {"&&"}, {"==", "!="}, {"<", "<=", ">", ">="}}

func (p *aparser) binary(level int) (anode, *SyntaxError) {
	if level == len(levels) {
		return p.unary()
	}
	l, err := p.binary(level + 1)
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.kind != atOp || !slices.Contains(levels[level], t.text) {
			return l, nil
		}
		p.i++
		r, err := p.binary(level + 1)
		if err != nil {
			return nil, err
		}
		l = abinary{op: t.text, l: l, r: r}
	}
}

func (p *aparser) unary() (anode, *SyntaxError) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxActionsDepth {
		return nil, &SyntaxError{Pos: p.peek().pos, Msg: fmt.Sprintf("nested more than %d levels deep", maxActionsDepth)}
	}
	if p.accept(atOp, "!") {
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return anot{x: x}, nil
	}
	return p.postfix()
}

func (p *aparser) postfix() (anode, *SyntaxError) {
	n, ref, err := p.primary()
	if err != nil {
		return nil, err
	}
	for {
		var seg asegment
		switch {
		case p.accept(atPunct, "."):
			t := p.next()
			switch {
			case t.kind == atIdent:
				seg.name = strings.ToLower(t.text)
			case t.kind == atPunct && t.text == "*":
				seg.star = true
			default:
				return nil, unexpected(t)
			}
		case p.accept(atPunct, "["):
			if p.accept(atPunct, "*") {
				seg.star = true
			} else {
				x, err := p.binary(0)
				if err != nil {
					return nil, err
				}
				seg.index = x
			}
			if err := p.expect("]"); err != nil {
				return nil, err
			}
		default:
			if ref != nil {
				return *ref, nil
			}
			return n, nil
		}
		if ref != nil {
			ref.path = append(ref.path, seg)
		} else {
			n = aaccess{x: n}
		}
	}
}

// primary returns either a node, or a named value that property accesses
// extend.
func (p *aparser) primary() (anode, *aref, *SyntaxError) {
	t := p.next()
	switch t.kind {
	case atNum:
		return aliteral{v: aval{kind: aNum, n: t.n}}, nil, nil
	case atStr:
		return aliteral{v: aval{kind: aStr, s: t.s}}, nil, nil
	case atIdent:
		switch t.text {
		case "true", "false":
			return aliteral{v: boolVal(t.text == "true")}, nil, nil
		case "null":
			return aliteral{v: aval{kind: aNull}}, nil, nil
		}
		if p.accept(atPunct, "(") {
			n, err := p.call(t)
			return n, nil, err
		}
		return nil, &aref{root: strings.ToLower(t.text)}, nil
	case atPunct:
		if t.text == "(" {
			n, err := p.binary(0)
			if err != nil {
				return nil, nil, err
			}
			if err := p.expect(")"); err != nil {
				return nil, nil, err
			}
			return n, nil, nil
		}
	}
	return nil, nil, unexpected(t)
}

func (p *aparser) call(name atok) (anode, *SyntaxError) {
	var args []anode
	var firstPos int
	if !p.accept(atPunct, ")") {
		for {
			if len(args) == 0 {
				firstPos = p.peek().pos
			}
			x, err := p.binary(0)
			if err != nil {
				return nil, err
			}
			args = append(args, x)
			if p.accept(atPunct, ")") {
				break
			}
			if err := p.expect(","); err != nil {
				return nil, err
			}
		}
	}
	key := strings.ToLower(name.text)
	f, ok := functions[key]
	if !ok {
		return nil, &SyntaxError{Pos: name.pos, Msg: fmt.Sprintf("unknown function %s", name.text)}
	}
	if len(args) < f.min || (f.max >= 0 && len(args) > f.max) {
		return nil, &SyntaxError{Pos: name.pos, Msg: fmt.Sprintf("%s: wrong number of arguments (%d)", f.display, len(args))}
	}
	// A format string written as a literal is checked here, where GitHub
	// would fail the run.
	if key == "format" {
		if l, ok := args[0].(aliteral); ok && l.v.kind == aStr {
			if _, err := formatString(l.v.s, make([]string, len(args)-1)); err != nil {
				return nil, &SyntaxError{Pos: firstPos, Msg: "format: " + err.Error()}
			}
		}
	}
	return acall{name: key, args: args}, nil
}
