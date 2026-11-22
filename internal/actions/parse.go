package actions

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/damienaulombard/pipewise/internal/config"
)

// ReadFunc reads a file of the repository by its slash-separated path from
// the repository root, as in `.github/workflows/build.yml`.
type ReadFunc func(path string) ([]byte, error)

// RepositoryRoot returns the repository holding a workflow file: the
// directory above `.github/workflows/`. ok is false when the file is not
// directly in such a directory.
func RepositoryRoot(file string) (root string, ok bool) {
	dir := filepath.Dir(filepath.Clean(file))
	if filepath.Base(dir) != "workflows" || filepath.Base(filepath.Dir(dir)) != ".github" {
		return "", false
	}
	return filepath.Dir(filepath.Dir(dir)), true
}

// DirReader reads files under root, never outside it, symbolic links
// included.
func DirReader(root string) ReadFunc {
	return func(name string) ([]byte, error) {
		r, err := os.OpenRoot(root)
		if err != nil {
			return nil, err
		}
		defer func() { _ = r.Close() }()
		return r.ReadFile(filepath.FromSlash(name))
	}
}

// Parse reads a workflow. It never fails: what cannot be read is reported in
// Workflow.Problems and the rest is still returned. read finds the local
// reusable workflows jobs call; with a nil read, they are reported as
// unresolved.
func Parse(data []byte, read ReadFunc) *Workflow {
	p := &parser{w: &Workflow{}, read: read, called: map[string]*Workflow{}}
	p.parse(data)
	return p.w
}

// maxNested is how many levels of reusable workflows GitHub runs below the
// caller: ten levels of workflows in all.
const maxNested = 9

type parser struct {
	w    *Workflow
	read ReadFunc
	// stack holds the local workflows being read, to detect a cycle and
	// limit the nesting.
	stack []string
	// called holds every local workflow already read, shared by the whole
	// tree of calls: each file is read, and its problems reported, once.
	called map[string]*Workflow
	// in holds, for each problem, the called file it was found in, empty
	// for this file. Such a problem's message already says where it is.
	in []string
}

func (p *parser) problem(kind, job string, line int, format string, args ...any) {
	p.add(config.Problem{Kind: kind, Job: job, Line: line, Message: fmt.Sprintf(format, args...)}, "")
}

func (p *parser) add(pb config.Problem, in string) {
	p.w.Problems = append(p.w.Problems, pb)
	p.in = append(p.in, in)
}

func (p *parser) parseError(job string, line int, format string, args ...any) {
	p.problem(config.KindParseError, job, line, format, args...)
}

// deref follows YAML aliases to the node they stand for.
func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

func isNull(n *yaml.Node) bool {
	return n == nil || n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

// pair is a key of a mapping and its value, aliases followed.
type pair struct {
	key   *yaml.Node
	value *yaml.Node
}

func pairs(m *yaml.Node) []pair {
	var out []pair
	for i := 0; i+1 < len(m.Content); i += 2 {
		out = append(out, pair{key: m.Content[i], value: deref(m.Content[i+1])})
	}
	return out
}

func lookup(m *yaml.Node, key string) (pair, bool) {
	for _, kv := range pairs(m) {
		if kv.key.Value == key {
			return kv, true
		}
	}
	return pair{}, false
}

// scalars returns a scalar as a one-element list, or a list of scalars.
// ok is false for anything else.
func scalars(n *yaml.Node) (list []string, ok bool) {
	switch n.Kind {
	case yaml.ScalarNode:
		return []string{n.Value}, true
	case yaml.SequenceNode:
		for _, item := range n.Content {
			item = deref(item)
			if item.Kind != yaml.ScalarNode {
				return nil, false
			}
			list = append(list, item.Value)
		}
		return list, true
	default:
		return nil, false
	}
}

func (p *parser) parse(data []byte) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		p.parseError("", 0, "invalid YAML: %v", err)
		return
	}
	if len(doc.Content) == 0 {
		p.parseError("", 0, "the workflow is empty")
		return
	}
	root := deref(doc.Content[0])
	if root.Kind != yaml.MappingNode {
		p.parseError("", root.Line, "the workflow must be a mapping of keywords")
		return
	}
	if kv, ok := lookup(root, "name"); ok && kv.value.Kind == yaml.ScalarNode {
		p.w.Name = kv.value.Value
	}
	if kv, ok := lookup(root, "on"); ok {
		p.parseOn(kv)
	} else {
		p.parseError("", 0, "the workflow has no on: keyword, so nothing starts it")
	}
	if kv, ok := lookup(root, "jobs"); ok {
		p.parseJobs(kv)
	} else {
		p.parseError("", 0, "the workflow has no jobs: keyword")
	}
}

func (p *parser) parseOn(on pair) {
	line := on.key.Line
	v := on.value
	before := len(p.w.Problems)
	switch {
	case isNull(v):
	case v.Kind == yaml.ScalarNode:
		p.w.Triggers = append(p.w.Triggers, Trigger{Event: v.Value, Line: v.Line})
	case v.Kind == yaml.SequenceNode:
		for _, item := range v.Content {
			item = deref(item)
			if item.Kind != yaml.ScalarNode {
				p.parseError("", line, "on: must be an event, a list of events or a mapping")
				return
			}
			p.w.Triggers = append(p.w.Triggers, Trigger{Event: item.Value, Line: item.Line})
		}
	case v.Kind == yaml.MappingNode:
		for _, kv := range pairs(v) {
			if t, ok := p.trigger(kv); ok {
				p.w.Triggers = append(p.w.Triggers, t)
			}
		}
	}
	if len(p.w.Triggers) == 0 && len(p.w.Problems) == before {
		p.parseError("", line, "on: names no event")
	}
}

// trigger reads one event of an `on:` mapping. Only `schedule` takes a
// list; the other events take nothing or a mapping of filters.
func (p *parser) trigger(kv pair) (Trigger, bool) {
	t := Trigger{Event: kv.key.Value, Line: kv.key.Line}
	v := kv.value
	switch {
	case isNull(v):
		return t, true
	case v.Kind == yaml.SequenceNode && t.Event == "schedule":
		return t, true
	case v.Kind != yaml.MappingNode:
		p.parseError("", t.Line, "on: %s must be empty or a mapping of filters", t.Event)
		return t, false
	}
	filters := map[string]*[]string{
		"branches": &t.Branches, "branches-ignore": &t.BranchesIgnore,
		"tags": &t.Tags, "tags-ignore": &t.TagsIgnore,
	}
	ok := true
	for _, f := range pairs(v) {
		name := f.key.Value
		if name == "paths" || name == "paths-ignore" {
			t.Paths = true
			continue
		}
		dst, known := filters[name]
		if !known {
			continue
		}
		list, valid := scalars(f.value)
		if !valid {
			p.parseError("", f.key.Line, "on: %s: %s must be a pattern or a list of patterns", t.Event, name)
			ok = false
			continue
		}
		*dst = list
	}
	return t, ok
}

func (p *parser) parseJobs(jobs pair) {
	line := jobs.key.Line
	v := jobs.value
	if v.Kind != yaml.MappingNode {
		p.parseError("", line, "jobs: must be a mapping of job ids to jobs")
		return
	}
	entries := pairs(v)
	if len(entries) == 0 {
		p.parseError("", line, "jobs: must define at least one job")
		return
	}
	ids := map[string]bool{}
	for _, kv := range entries {
		ids[kv.key.Value] = true
	}
	for _, kv := range entries {
		name := kv.key.Value
		if kv.value.Kind != yaml.MappingNode {
			p.parseError(name, kv.key.Line, "job %q must be a mapping", name)
			continue
		}
		p.w.Jobs = append(p.w.Jobs, p.job(name, kv.key.Line, kv.value, ids))
	}
}

func (p *parser) job(name string, line int, m *yaml.Node, ids map[string]bool) *Job {
	j := &Job{Name: name, Line: line}
	if kv, ok := lookup(m, "needs"); ok && !isNull(kv.value) {
		needs, valid := scalars(kv.value)
		if !valid {
			p.parseError(name, kv.key.Line, "needs: of job %q must be a job id or a list of job ids", name)
		}
		for _, n := range needs {
			if !ids[n] {
				p.parseError(name, kv.key.Line, "job %q needs %q, which is not a job of this workflow", name, n)
			}
		}
		j.Needs = needs
	}
	if kv, ok := lookup(m, "if"); ok && !isNull(kv.value) {
		if kv.value.Kind != yaml.ScalarNode {
			p.parseError(name, kv.key.Line, "if: of job %q must be an expression", name)
		} else {
			j.If = kv.value.Value
		}
	}
	_, hasRunner := lookup(m, "runs-on")
	steps, hasSteps := lookup(m, "steps")
	if kv, ok := lookup(m, "uses"); ok {
		if hasRunner || hasSteps {
			p.parseError(name, line, "job %q calls a reusable workflow, so it cannot have steps or runs-on", name)
		}
		if kv.value.Kind != yaml.ScalarNode || isNull(kv.value) {
			p.parseError(name, kv.key.Line, "uses: of job %q must be a string", name)
			return j
		}
		j.Uses = p.call(name, kv.key.Line, kv.value.Value)
		return j
	}
	if !hasRunner {
		p.parseError(name, line, "job %q needs runs-on, or uses: for a reusable workflow", name)
	}
	if !hasSteps {
		p.parseError(name, line, "job %q needs steps:", name)
		return j
	}
	j.Steps = p.steps(name, steps)
	return j
}

func (p *parser) steps(job string, kv pair) []Step {
	if kv.value.Kind != yaml.SequenceNode {
		p.parseError(job, kv.key.Line, "steps: of job %q must be a list", job)
		return nil
	}
	var out []Step
	for i, item := range kv.value.Content {
		item = deref(item)
		n := i + 1
		if item.Kind != yaml.MappingNode {
			p.parseError(job, item.Line, "step %d of job %q must be a mapping", n, job)
			continue
		}
		s := Step{Line: item.Line}
		if name, ok := lookup(item, "name"); ok && name.value.Kind == yaml.ScalarNode {
			s.Name = name.value.Value
		}
		uses, hasUses := lookup(item, "uses")
		_, s.Run = lookup(item, "run")
		switch {
		case hasUses && s.Run:
			p.parseError(job, item.Line, "step %d of job %q has both uses: and run:", n, job)
		case hasUses && uses.value.Kind != yaml.ScalarNode:
			p.parseError(job, item.Line, "step %d of job %q: uses: must be a string", n, job)
		case hasUses:
			s.Uses = uses.value.Value
		case !s.Run:
			p.parseError(job, item.Line, "step %d of job %q needs uses: or run:", n, job)
		}
		out = append(out, s)
	}
	return out
}

// call reads the `uses:` of a job. A local workflow is parsed with the same
// reader, once however many jobs call it; its problems are reported on the
// first calling job, with their place in the file they are in.
func (p *parser) call(job string, line int, ref string) *Call {
	c := &Call{Ref: ref, Line: line}
	if !strings.HasPrefix(ref, "./") {
		if !strings.Contains(ref, "@") {
			p.parseError(job, line, "uses: %s: a workflow in another repository needs @ref", ref)
			return c
		}
		p.problem(KindUnresolvedWorkflow, job, line,
			"uses: %s is a workflow in another repository; pipewise does not fetch it, so its jobs are not analysed", ref)
		return c
	}
	c.Local = true
	if strings.Contains(ref, "@") {
		p.parseError(job, line, "uses: %s: a workflow in the same repository takes no @ref", ref)
		return c
	}
	name := path.Clean(strings.TrimPrefix(ref, "./"))
	if name == ".." || strings.HasPrefix(name, "../") {
		p.problem(KindUnresolvedWorkflow, job, line, "uses: %s points outside the repository; its jobs are not analysed", ref)
		return c
	}
	if p.read == nil {
		p.problem(KindUnresolvedWorkflow, job, line,
			"uses: %s: the repository is unknown (standard input, or a file outside .github/workflows); its jobs are not analysed", ref)
		return c
	}
	if slices.Contains(p.stack, name) {
		p.problem(KindUnresolvedWorkflow, job, line, "uses: %s calls itself, directly or through another workflow", ref)
		return c
	}
	if w, ok := p.called[name]; ok {
		c.Workflow = w
		return c
	}
	if len(p.stack) >= maxNested {
		p.parseError(job, line, "uses: %s: GitHub allows at most ten levels of workflows, the caller included", ref)
		return c
	}
	data, err := p.read(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		p.problem(KindUnresolvedWorkflow, job, line, "uses: %s: no such file in the repository; its jobs are not analysed", ref)
		return c
	case err != nil:
		p.problem(KindUnresolvedWorkflow, job, line, "uses: %s: cannot read it (%v); its jobs are not analysed", ref, err)
		return c
	}
	sub := &parser{w: &Workflow{}, read: p.read, stack: append(slices.Clone(p.stack), name), called: p.called}
	p.called[name] = sub.w
	sub.parse(data)
	for i, pb := range sub.w.Problems {
		msg, in := pb.Message, sub.in[i]
		if in == "" {
			in = name
			where := name
			if pb.Line > 0 {
				where = fmt.Sprintf("%s:%d", name, pb.Line)
			}
			msg = fmt.Sprintf("in %s: %s", where, pb.Message)
		}
		p.add(config.Problem{Kind: pb.Kind, Job: job, Line: line, Message: msg}, in)
	}
	c.Workflow = sub.w
	return c
}
