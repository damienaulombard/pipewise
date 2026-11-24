// Package fix writes the fixes of the findings that cannot change what a
// pipeline does, as a unified diff of the configuration file. The file itself
// is never modified: only lines are added, and every other line is kept as
// written, comments and key order included.
package fix

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/damienaulombard/pipewise/internal/rules"
)

// DefaultExpireIn is the expire_in added to artifacts that have none, the
// value the artifacts-no-expiry suggestion prints.
const DefaultExpireIn = "1 week"

const (
	ruleInterruptible = "missing-interruptible"
	ruleArtifacts     = "artifacts-no-expiry"
)

// Fixable tells the rules whose findings Fix knows how to fix.
func Fixable(rule string) bool {
	return rule == ruleInterruptible || rule == ruleArtifacts
}

// Options tune the fixes.
type Options struct {
	// ExpireIn is the expire_in added to artifacts; DefaultExpireIn when
	// empty.
	ExpireIn string
}

// Skip is a finding Fix could not fix, and why.
type Skip struct {
	Rule string
	// Job is empty for the finding on `default:`.
	Job    string
	Line   int
	Reason string
}

// Result is the outcome of Fix.
type Result struct {
	// Fixed is the file with the fixes applied; the input when there are
	// none.
	Fixed []byte
	// Diff is the unified diff from the input to Fixed, empty when there is
	// nothing to fix.
	Diff string
	// Skipped lists the fixable findings left as they are, in the order of
	// the findings.
	Skipped []Skip
}

// insertion is lines added after line `after` of the file (1-based).
type insertion struct {
	after int
	text  []string
}

// Fix applies the fixes of the findings of Fixable rules to data, the file
// at path, and returns the diff. Findings of other rules are ignored. A
// finding whose fix would have to rewrite a line, or to change a place shared
// with what is not reported, is skipped with the reason.
func Fix(path string, data []byte, findings []rules.Finding, opts Options) Result {
	res := Result{Fixed: data}
	expireIn := opts.ExpireIn
	if expireIn == "" {
		expireIn = DefaultExpireIn
	}
	lines := splitLines(data)
	f := newFixer(configRoot(data), lines)

	added := map[int][]string{}
	for _, finding := range findings {
		if !Fixable(finding.Rule) {
			continue
		}
		var ins insertion
		var err error
		if finding.Rule == ruleInterruptible {
			ins, err = f.interruptible(finding.Job)
		} else {
			ins, err = f.expireIn(finding.Job, scalar(expireIn))
		}
		if err != nil {
			res.Skipped = append(res.Skipped, Skip{
				Rule: finding.Rule, Job: finding.Job, Line: finding.Line, Reason: err.Error(),
			})
			continue
		}
		added[ins.after] = appendNew(added[ins.after], ins.text...)
	}
	if len(added) == 0 {
		return res
	}

	all := make([]insertion, 0, len(added))
	for after, text := range added {
		all = append(all, insertion{after: after, text: text})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].after < all[j].after })
	res.Fixed = apply(lines, all)
	res.Diff = unified(path, lines, all)
	return res
}

// appendNew appends the lines of text that list does not hold yet: two
// findings can share one fix, as jobs sharing an anchored artifacts:.
func appendNew(list []string, text ...string) []string {
	for _, t := range text {
		found := false
		for _, l := range list {
			found = found || l == t
		}
		if !found {
			list = append(list, t)
		}
	}
	return list
}

// scalar writes value as a YAML scalar, quoted only when it must be.
func scalar(value string) string {
	out, err := yaml.Marshal(value)
	if err != nil {
		// A string always marshals; keep the value as given otherwise.
		return value
	}
	return strings.TrimSuffix(string(out), "\n")
}

// configRoot returns the mapping holding the configuration, or nil when the
// file cannot be read as one. Like config.Parse, it skips the `spec:` header
// of a configuration with inputs.
func configRoot(data []byte) *yaml.Node {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*yaml.Node
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil
		}
		docs = append(docs, &doc)
	}
	if len(docs) == 0 || len(docs[0].Content) == 0 {
		return nil
	}
	root := docs[0].Content[0]
	if root.Kind == yaml.MappingNode && len(root.Content) == 2 && root.Content[0].Value == "spec" &&
		len(docs) > 1 && len(docs[1].Content) > 0 {
		root = docs[1].Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	return root
}

// fixer finds where each fix goes in the YAML tree of the file.
type fixer struct {
	root  *yaml.Node
	lines []string
	// keyOf maps a value node to the key node it is written under.
	keyOf map[*yaml.Node]*yaml.Node
}

func newFixer(root *yaml.Node, lines []string) *fixer {
	f := &fixer{root: root, lines: lines, keyOf: map[*yaml.Node]*yaml.Node{}}
	if root != nil {
		f.index(root)
	}
	return f
}

// index records the key of every mapping value. Aliases are not followed:
// their target is indexed where it is written.
func (f *fixer) index(n *yaml.Node) {
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			f.keyOf[n.Content[i+1]] = n.Content[i]
		}
	}
	for _, c := range n.Content {
		f.index(c)
	}
}

var (
	errMerged  = errors.New("merge key")
	errMissing = errors.New("missing key")
)

// lookup returns the key and value written for key in the mapping m. A key
// that only a merge key (`<<:`) brings in returns errMerged: the fix would
// belong to the anchored mapping, shared with whatever else merges it.
func lookup(m *yaml.Node, key string) (k, v *yaml.Node, err error) {
	var sources []*yaml.Node
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Tag == "!!merge" {
			sources = append(sources, m.Content[i+1])
			continue
		}
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1], nil
		}
	}
	for len(sources) > 0 {
		s := sources[0]
		sources = sources[1:]
		if s.Kind == yaml.AliasNode {
			s = s.Alias
		}
		switch s.Kind {
		case yaml.SequenceNode:
			sources = append(sources, s.Content...)
		case yaml.MappingNode:
			if _, _, err := lookup(s, key); !errors.Is(err, errMissing) {
				return nil, nil, errMerged
			}
		}
	}
	return nil, nil, errMissing
}

// job returns the mapping of a job, or why the fix cannot go into it.
func (f *fixer) job(name string) (k, v *yaml.Node, err error) {
	if f.root == nil {
		return nil, nil, errors.New("the configuration could not be read")
	}
	k, v, err = lookup(f.root, name)
	switch {
	case errors.Is(err, errMerged):
		return nil, nil, fmt.Errorf("%q comes from a merge key (<<:); fix the mapping it merges", name)
	case err != nil:
		return nil, nil, fmt.Errorf("%q is not defined in this file; it may come from an included file", name)
	case v.Kind == yaml.AliasNode:
		return nil, nil, fmt.Errorf("%q is an alias of another mapping, shared with whatever else uses that anchor", name)
	}
	return k, v, nil
}

// interruptible adds `interruptible: true` to the job itself: an extends
// parent may be shared with jobs that must not be interrupted.
func (f *fixer) interruptible(name string) (insertion, error) {
	k, v, err := f.job(name)
	if err != nil {
		return insertion{}, err
	}
	return f.addKey(k, v, "interruptible: true", fmt.Sprintf("job %q is", name))
}

// expireIn adds expire_in under the artifacts: a job gets, or under
// `default: artifacts:` for the finding on `default:` (no job).
func (f *fixer) expireIn(name, value string) (insertion, error) {
	var k, v *yaml.Node
	var err error
	what := fmt.Sprintf("the artifacts of %q are", name)
	if name == "" {
		what = "the default artifacts are"
		k, v, err = f.defaultArtifacts()
	} else {
		k, v, err = f.artifacts(name, 0)
	}
	if err != nil {
		return insertion{}, err
	}
	// `artifacts: *bin` is fixed where the anchor is written, once for
	// every job using it.
	if v.Kind == yaml.AliasNode {
		v = v.Alias
		if k = f.keyOf[v]; k == nil {
			return insertion{}, fmt.Errorf("%s an alias of an anchor that is not under a key", what)
		}
	}
	return f.addKey(k, v, "expire_in: "+value, what)
}

func (f *fixer) defaultArtifacts() (k, v *yaml.Node, err error) {
	if f.root == nil {
		return nil, nil, errors.New("the configuration could not be read")
	}
	_, d, err := lookup(f.root, "default")
	if err == nil && d.Kind == yaml.AliasNode {
		d = d.Alias
	}
	if err != nil || d.Kind != yaml.MappingNode {
		return nil, nil, errors.New("default: is not a mapping written in this file")
	}
	k, v, err = lookup(d, "artifacts")
	switch {
	case errors.Is(err, errMerged):
		return nil, nil, errors.New("the default artifacts come from a merge key (<<:); fix the mapping it merges")
	case err != nil:
		return nil, nil, errors.New("default: has no artifacts written in this file")
	}
	return k, v, nil
}

// maxExtends bounds the depth of extends followed, as GitLab does (11
// levels); it also stops a cycle, which config reports.
const maxExtends = 11

// artifacts returns the artifacts: a job gets through extends: its own, or
// those of the last parent listed that has some, at any depth. It returns
// errMissing when none of them has artifacts.
func (f *fixer) artifacts(name string, depth int) (k, v *yaml.Node, err error) {
	if depth > maxExtends {
		return nil, nil, errors.New("extends is nested too deeply")
	}
	_, m, err := f.job(name)
	if err != nil {
		return nil, nil, err
	}
	if m.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("%q is not a mapping", name)
	}
	k, v, err = lookup(m, "artifacts")
	switch {
	case err == nil:
		return k, v, nil
	case errors.Is(err, errMerged):
		return nil, nil, fmt.Errorf("the artifacts of %q come from a merge key (<<:); fix the mapping it merges", name)
	}
	_, ext, err := lookup(m, "extends")
	if err != nil {
		return nil, nil, errMissing
	}
	if ext.Kind == yaml.AliasNode {
		ext = ext.Alias
	}
	parents := []*yaml.Node{ext}
	if ext.Kind == yaml.SequenceNode {
		parents = ext.Content
	}
	// Later parents override earlier ones.
	for i := len(parents) - 1; i >= 0; i-- {
		k, v, err = f.artifacts(parents[i].Value, depth+1)
		if !errors.Is(err, errMissing) {
			return k, v, err
		}
	}
	return nil, nil, errMissing
}

// addKey inserts line as the first entry of the block mapping v, written
// under the key k, right below k and at the indentation of v's entries.
func (f *fixer) addKey(k, v *yaml.Node, line, what string) (insertion, error) {
	switch {
	case v.Kind != yaml.MappingNode:
		return insertion{}, fmt.Errorf("%s not a mapping", what)
	case v.Style&yaml.FlowStyle != 0, len(v.Content) == 0, v.Content[0].Line <= k.Line:
		return insertion{}, fmt.Errorf("%s written on one line ({...}); adding a key would rewrite it", what)
	}
	eol := "\n"
	if strings.HasSuffix(f.lines[k.Line-1], "\r\n") {
		eol = "\r\n"
	}
	indent := strings.Repeat(" ", v.Content[0].Column-1)
	return insertion{after: k.Line, text: []string{indent + line + eol}}, nil
}

// splitLines splits data into lines that keep their line ending.
func splitLines(data []byte) []string {
	lines := strings.SplitAfter(string(data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// apply returns lines with ins (sorted by line) added.
func apply(lines []string, ins []insertion) []byte {
	var b strings.Builder
	next := 0
	for i := 0; i <= len(lines); i++ {
		if i > 0 {
			b.WriteString(lines[i-1])
		}
		for next < len(ins) && ins[next].after == i {
			for _, t := range ins[next].text {
				b.WriteString(t)
			}
			next++
		}
	}
	return []byte(b.String())
}

// contextLines is the number of unchanged lines around each change, as in
// `diff -u`.
const contextLines = 3

// unified writes the diff that adds ins (sorted by line) to lines, with
// the a/ and b/ prefixes `git apply` and `patch -p1` expect.
func unified(path string, lines []string, ins []insertion) string {
	type hunk struct {
		start, end int // old lines, 1-based, inclusive
		ins        []insertion
	}
	var hunks []hunk
	for _, in := range ins {
		start := max(1, in.after-contextLines+1)
		end := min(len(lines), in.after+contextLines)
		if n := len(hunks); n > 0 && start <= hunks[n-1].end+1 {
			hunks[n-1].end = max(hunks[n-1].end, end)
			hunks[n-1].ins = append(hunks[n-1].ins, in)
			continue
		}
		hunks = append(hunks, hunk{start: start, end: end, ins: []insertion{in}})
	}

	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", path, path)
	shift := 0
	for _, h := range hunks {
		added := 0
		for _, in := range h.ins {
			added += len(in.text)
		}
		old := h.end - h.start + 1
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", h.start, old, h.start+shift, old+added)
		next := 0
		for i := h.start - 1; i <= h.end; i++ {
			if i >= h.start {
				writeLine(&b, " ", lines[i-1])
			}
			for next < len(h.ins) && h.ins[next].after == i {
				for _, t := range h.ins[next].text {
					writeLine(&b, "+", t)
				}
				next++
			}
		}
		shift += added
	}
	return b.String()
}

func writeLine(b *strings.Builder, prefix, line string) {
	b.WriteString(prefix)
	b.WriteString(line)
	if !strings.HasSuffix(line, "\n") {
		b.WriteString("\n\\ No newline at end of file\n")
	}
}
