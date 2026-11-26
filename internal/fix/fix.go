// Package fix writes the fixes of findings as a unified diff of the
// configuration file. The file itself is never modified, and every line a fix
// does not touch is kept as written, comments and key order included.
//
// The fixes of missing-interruptible and artifacts-no-expiry cannot change
// what a pipeline does: they only add lines. The fix of redundant-job is the
// only one that changes when a job runs: it rewrites the job's rules: (or
// replaces its only:/except:), and is written only when the caller states
// that the project meets the preconditions that make it safe.
package fix

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/damienaulombard/pipewise/internal/eval"
	"github.com/damienaulombard/pipewise/internal/rules"
)

// DefaultExpireIn is the expire_in added to artifacts that have none, the
// value the artifacts-no-expiry suggestion prints.
const DefaultExpireIn = "1 week"

const (
	ruleInterruptible = "missing-interruptible"
	ruleArtifacts     = "artifacts-no-expiry"
	ruleRedundant     = "redundant-job"
)

// Fixable tells the rules whose findings Fix knows how to fix.
func Fixable(rule string) bool {
	return rule == ruleInterruptible || rule == ruleArtifacts || rule == ruleRedundant
}

// Options tune the fixes.
type Options struct {
	// ExpireIn is the expire_in added to artifacts; DefaultExpireIn when
	// empty.
	ExpireIn string
	// MergedResultsPipelines and ProtectedDefaultBranch are the project
	// settings that make the redundant-job fix safe: merged results
	// pipelines are enabled, and nobody pushes to the default branch
	// directly. redundant-job is fixed only when both are True.
	MergedResultsPipelines eval.Truth
	ProtectedDefaultBranch eval.Truth
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

// edit removes the `del` lines that follow line `after` of the file
// (1-based; 0 is the top of the file), and writes text in their place.
type edit struct {
	after, del int
	text       []string
}

// Fix applies the fixes of the findings of Fixable rules in outcome to data,
// the file at path, and returns the diff. Findings of other rules are
// ignored. A finding whose fix would have to rewrite a line it does not own,
// or to change a place shared with what is not reported, is skipped with the
// reason, as is a redundant-job finding when opts do not state both of its
// preconditions.
func Fix(path string, data []byte, outcome *rules.Outcome, opts Options) Result {
	res := Result{Fixed: data}
	expireIn := opts.ExpireIn
	if expireIn == "" {
		expireIn = DefaultExpireIn
	}
	lines := splitLines(data)
	f := newFixer(configRoot(data), lines)
	jobs := map[string]eval.JobResult{}
	for _, jr := range outcome.Jobs {
		jobs[jr.Job.Name] = jr
	}

	edits := map[int]edit{}
	for _, finding := range outcome.Findings {
		if !Fixable(finding.Rule) {
			continue
		}
		var es []edit
		var err error
		switch finding.Rule {
		case ruleInterruptible:
			es, err = one(f.interruptible(finding.Job))
		case ruleArtifacts:
			es, err = one(f.expireIn(finding.Job, scalar(expireIn)))
		default:
			es, err = f.redundant(finding.Job, jobs, opts)
		}
		if err == nil {
			edits, err = placeAll(edits, es)
		}
		if err != nil {
			res.Skipped = append(res.Skipped, Skip{
				Rule: finding.Rule, Job: finding.Job, Line: finding.Line, Reason: err.Error(),
			})
		}
	}
	if len(edits) == 0 {
		return res
	}

	all := make([]edit, 0, len(edits))
	for _, e := range edits {
		all = append(all, e)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].after < all[j].after })
	res.Fixed = apply(lines, all)
	res.Diff = unified(path, lines, all)
	return res
}

func one(e edit, err error) ([]edit, error) {
	return []edit{e}, err
}

var errOverlap = errors.New("another fix changes the same lines; fix this one by hand")

// placeAll adds every edit of es to edits, or none of them when one
// overlaps what is there.
func placeAll(edits map[int]edit, es []edit) (map[int]edit, error) {
	trial := maps.Clone(edits)
	for _, e := range es {
		if err := place(trial, e); err != nil {
			return edits, err
		}
	}
	return trial, nil
}

// place adds e to edits. Edits at the same place are merged, as long as at
// most one of them removes lines; an edit cannot start among the lines
// another one removes.
func place(edits map[int]edit, e edit) error {
	for _, x := range edits {
		if x.after == e.after && x.del > 0 && e.del > 0 || inside(e, x) || inside(x, e) {
			return errOverlap
		}
	}
	x := edits[e.after]
	x.after = e.after
	x.del += e.del
	x.text = appendNew(x.text, e.text...)
	edits[e.after] = x
	return nil
}

// inside tells whether a starts among the lines b removes, its last one
// excepted: writing right after them is fine.
func inside(a, b edit) bool {
	return b.after < a.after && a.after < b.after+b.del
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
func (f *fixer) interruptible(name string) (edit, error) {
	k, v, err := f.job(name)
	if err != nil {
		return edit{}, err
	}
	return f.addKey(k, v, "interruptible: true", fmt.Sprintf("job %q is", name))
}

// expireIn adds expire_in under the artifacts: a job gets, or under
// `default: artifacts:` for the finding on `default:` (no job).
func (f *fixer) expireIn(name, value string) (edit, error) {
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
		return edit{}, err
	}
	// `artifacts: *bin` is fixed where the anchor is written, once for
	// every job using it.
	if v.Kind == yaml.AliasNode {
		v = v.Alias
		if k = f.keyOf[v]; k == nil {
			return edit{}, fmt.Errorf("%s an alias of an anchor that is not under a key", what)
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
func (f *fixer) addKey(k, v *yaml.Node, line, what string) (edit, error) {
	if err := blockMapping(k, v, what); err != nil {
		return edit{}, err
	}
	return edit{after: k.Line, text: []string{indentOf(v) + line + f.eol(k.Line)}}, nil
}

// blockMapping checks that v, written under k, is a mapping written on lines
// of its own, where keys can be added and removed.
func blockMapping(k, v *yaml.Node, what string) error {
	switch {
	case v.Kind != yaml.MappingNode:
		return fmt.Errorf("%s not a mapping", what)
	case v.Style&yaml.FlowStyle != 0, len(v.Content) == 0, v.Content[0].Line <= k.Line:
		return fmt.Errorf("%s written on one line ({...}); adding a key would rewrite it", what)
	}
	return nil
}

// indentOf is the indentation of the entries of the block mapping v.
func indentOf(v *yaml.Node) string {
	return strings.Repeat(" ", v.Content[0].Column-1)
}

// eol is the line ending of line n (1-based), for the lines written there.
func (f *fixer) eol(n int) string {
	if strings.HasSuffix(f.lines[n-1], "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// Where to enable each precondition of the redundant-job fix.
const (
	howMergedResults = "enable them in Settings > Merge requests > Merge options > " +
		"Enable merged results pipelines, then pass --merged-results-pipelines=true"
	howProtected = "in Settings > Repository > Protected branches, set Allowed to push and merge " +
		"to No one for the default branch, then pass --protected-default-branch=true"
)

// preconditions says which precondition of the redundant-job fix opts do
// not state, and how to meet it; empty when both are met.
func preconditions(opts Options) string {
	var missing []string
	switch opts.MergedResultsPipelines {
	case eval.True:
	case eval.False:
		missing = append(missing, "merged results pipelines are not enabled (--merged-results-pipelines=false): "+howMergedResults)
	default:
		missing = append(missing, "merged results pipelines are not known to be enabled (--merged-results-pipelines=unknown): "+howMergedResults)
	}
	switch opts.ProtectedDefaultBranch {
	case eval.True:
	case eval.False:
		missing = append(missing, "direct pushes to the default branch are allowed (--protected-default-branch=false): "+howProtected)
	default:
		missing = append(missing, "direct pushes to the default branch are not known to be refused (--protected-default-branch=unknown): "+howProtected)
	}
	return strings.Join(missing, "; ")
}

// entry is a key of a job and the lines its value spans.
type entry struct {
	key        string
	start, end int
}

// redundant keeps a job reported by redundant-job on the first event it
// runs on: its own rules: is replaced, its own only: and except: are
// removed, and the new rules: goes where the first of them was, or first in
// the job when it has none (a rules: from extends is then overridden).
func (f *fixer) redundant(name string, jobs map[string]eval.JobResult, opts Options) ([]edit, error) {
	if missing := preconditions(opts); missing != "" {
		return nil, errors.New(missing)
	}
	jr, ok := jobs[name]
	if !ok {
		return nil, fmt.Errorf("%q is not a job of this configuration", name)
	}
	cond, ok := rules.RedundantCondition(jr)
	if !ok {
		return nil, fmt.Errorf("%q does not surely run on any event", name)
	}
	k, v, err := f.job(name)
	if err != nil {
		return nil, err
	}
	if err := blockMapping(k, v, fmt.Sprintf("job %q is", name)); err != nil {
		return nil, err
	}
	if other := f.neededBy(name); other != "" {
		return nil, fmt.Errorf("%q is in the needs: or dependencies: of %q, and GitLab rejects a pipeline "+
			"where a job needs one that is not in it; fix it by hand", name, other)
	}

	var own []entry
	for _, key := range []string{"rules", "only", "except"} {
		e, err := f.entry(k, v, name, key)
		switch {
		case errors.Is(err, errMissing):
			continue
		case err != nil:
			return nil, err
		}
		own = append(own, e)
	}
	inherited := []struct {
		key string
		set bool
	}{{"only", jr.Job.Only != nil}, {"except", jr.Job.Except != nil}}
	for _, in := range inherited {
		if key := in.key; in.set && !hasKey(own, key) {
			return nil, fmt.Errorf("%q gets %s: through extends, and GitLab rejects rules: next to it; "+
				"replace it with rules: in the parent", name, key)
		}
	}
	sort.Slice(own, func(i, j int) bool { return own[i].start < own[j].start })

	indent := indentOf(v)
	eol := f.eol(k.Line)
	text := []string{indent + "rules:" + eol, indent + "  - if: " + cond + eol}
	if len(own) == 0 {
		return []edit{{after: k.Line, text: text}}, nil
	}
	es := make([]edit, len(own))
	for i, e := range own {
		es[i] = edit{after: e.start - 1, del: e.end - e.start + 1}
	}
	es[0].text = text
	return es, nil
}

// neededBy returns a key of the file whose needs: or dependencies: names
// the job, empty when none does. Every top-level key is read, hidden jobs
// included, since a job can get its needs: from one through extends.
func (f *fixer) neededBy(name string) string {
	for i := 0; i+1 < len(f.root.Content); i += 2 {
		m := f.root.Content[i+1]
		if m.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(m.Content); j += 2 {
			if key := m.Content[j].Value; (key == "needs" || key == "dependencies") && names(m.Content[j+1], name) {
				return f.root.Content[i].Value
			}
		}
	}
	return ""
}

// names tells whether a needs: or dependencies: value names the job: as an
// item of the list, or as the job: of an item that is not about another
// pipeline or project.
func names(n *yaml.Node, job string) bool {
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	for _, item := range n.Content {
		if item.Kind == yaml.AliasNode {
			item = item.Alias
		}
		switch item.Kind {
		case yaml.ScalarNode:
			if item.Value == job {
				return true
			}
		case yaml.MappingNode:
			_, _, errPipeline := lookup(item, "pipeline")
			_, _, errProject := lookup(item, "project")
			_, j, err := lookup(item, "job")
			if errPipeline != nil && errProject != nil && err == nil && j.Value == job {
				return true
			}
		}
	}
	return false
}

func hasKey(entries []entry, key string) bool {
	for _, e := range entries {
		if e.key == key {
			return true
		}
	}
	return false
}

// entry returns the lines of key in the job mapping v written under k:
// from the key to the line before the next key, comments and blank lines
// at the end left out. It returns errMissing when the job has no such key.
func (f *fixer) entry(k, v *yaml.Node, name, key string) (entry, error) {
	var i int
	for i = 0; i+1 < len(v.Content); i += 2 {
		if v.Content[i].Value == key {
			break
		}
	}
	if i+1 >= len(v.Content) {
		if _, _, err := lookup(v, key); errors.Is(err, errMerged) {
			return entry{}, fmt.Errorf("the %s: of %q comes from a merge key (<<:); fix the mapping it merges", key, name)
		}
		return entry{}, errMissing
	}
	kk, vv := v.Content[i], v.Content[i+1]
	if err := movable(vv); err != nil {
		return entry{}, fmt.Errorf("the %s: of %q %w; fix it by hand", key, name, err)
	}
	if key == "rules" {
		if err := onlyIfs(vv, name); err != nil {
			return entry{}, err
		}
	}
	// A sequence may be written at the indentation of its key.
	indentless := vv.Kind == yaml.SequenceNode && vv.Style&yaml.FlowStyle == 0

	limit := len(f.lines) + 1
	if i+2 < len(v.Content) {
		limit = v.Content[i+2].Line
	} else {
		for j := 0; j+2 < len(f.root.Content); j += 2 {
			if f.root.Content[j] == k {
				limit = f.root.Content[j+2].Line
			}
		}
	}
	end := limit - 1
	for end > kk.Line && isBlankOrComment(f.lines[end-1]) {
		end--
	}
	for n := kk.Line + 1; n <= end; n++ {
		l := f.lines[n-1]
		indent := len(l) - len(strings.TrimLeft(l, " "))
		if indentless && indent == kk.Column-1 && strings.HasPrefix(strings.TrimSpace(l), "-") {
			continue
		}
		if !isBlankOrComment(l) && indent < kk.Column {
			return entry{}, fmt.Errorf("cannot tell where the %s: of %q ends (line %d); fix it by hand", key, name, n)
		}
	}
	return entry{key: key, start: kk.Line, end: end}, nil
}

// onlyIfs checks that every rule of a rules: value only sets if:, so that
// the rules: the fix writes drops nothing but the other events: a rule's
// variables:, when:, allow_failure: or changes: would be lost silently.
func onlyIfs(rulesValue *yaml.Node, name string) error {
	if rulesValue.Kind == yaml.AliasNode {
		rulesValue = rulesValue.Alias
	}
	if rulesValue.Kind != yaml.SequenceNode || rulesValue.Tag == "!reference" {
		return fmt.Errorf("the rules: of %q is not a list written in this file; fix it by hand", name)
	}
	for _, r := range rulesValue.Content {
		if r.Kind == yaml.AliasNode {
			r = r.Alias
		}
		ok := r.Kind == yaml.MappingNode
		for i := 0; ok && i < len(r.Content); i += 2 {
			ok = r.Content[i].Value == "if"
		}
		if !ok {
			return fmt.Errorf("a rule of %q sets more than if: (variables:, when:, changes:...), "+
				"which the fix would drop; fix it by hand", name)
		}
	}
	return nil
}

// movable checks that a value can be removed without changing anything
// else: it holds no anchor another node may use, and no block scalar, whose
// last lines could read as comments.
func movable(n *yaml.Node) error {
	switch {
	case n.Anchor != "":
		return fmt.Errorf("holds the anchor &%s, which other jobs may use", n.Anchor)
	case n.Kind == yaml.ScalarNode && n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		return errors.New("holds a block scalar (| or >)")
	}
	for _, c := range n.Content {
		if err := movable(c); err != nil {
			return err
		}
	}
	return nil
}

func isBlankOrComment(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

// splitLines splits data into lines that keep their line ending.
func splitLines(data []byte) []string {
	lines := strings.SplitAfter(string(data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// apply returns lines with edits (sorted by line) made.
func apply(lines []string, edits []edit) []byte {
	var b strings.Builder
	pos := 1
	for _, e := range edits {
		for ; pos <= e.after; pos++ {
			b.WriteString(lines[pos-1])
		}
		pos += e.del
		for _, t := range e.text {
			b.WriteString(t)
		}
	}
	for ; pos <= len(lines); pos++ {
		b.WriteString(lines[pos-1])
	}
	return []byte(b.String())
}

// contextLines is the number of unchanged lines around each change, as in
// `diff -u`.
const contextLines = 3

// unified writes the diff that makes edits (sorted by line) to lines, with
// the a/ and b/ prefixes `git apply` and `patch -p1` expect.
func unified(path string, lines []string, edits []edit) string {
	type hunk struct {
		start, end int // old lines, 1-based, inclusive
		edits      []edit
	}
	var hunks []hunk
	for _, e := range edits {
		start := max(1, e.after-contextLines+1)
		end := min(len(lines), e.after+e.del+contextLines)
		if n := len(hunks); n > 0 && start <= hunks[n-1].end+1 {
			hunks[n-1].end = max(hunks[n-1].end, end)
			hunks[n-1].edits = append(hunks[n-1].edits, e)
			continue
		}
		hunks = append(hunks, hunk{start: start, end: end, edits: []edit{e}})
	}

	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", path, path)
	shift := 0
	for _, h := range hunks {
		change := 0
		for _, e := range h.edits {
			change += len(e.text) - e.del
		}
		old := h.end - h.start + 1
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", h.start, old, h.start+shift, old+change)
		pos := h.start
		for _, e := range h.edits {
			for ; pos <= e.after; pos++ {
				writeLine(&b, " ", lines[pos-1])
			}
			for range e.del {
				writeLine(&b, "-", lines[pos-1])
				pos++
			}
			for _, t := range e.text {
				writeLine(&b, "+", t)
			}
		}
		for ; pos <= h.end; pos++ {
			writeLine(&b, " ", lines[pos-1])
		}
		shift += change
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
