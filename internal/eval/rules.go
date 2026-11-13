package eval

import (
	"fmt"
	"strings"

	"github.com/damienaulombard/pipewise/internal/config"
)

// Verdict says whether a job runs in the pipeline of one event.
type Verdict int

// Verdicts.
const (
	// Skipped: the job is not added to the pipeline.
	Skipped Verdict = iota
	// Runs: the job is added and runs on its own.
	Runs
	// Manual: the job is added and waits for someone to start it.
	Manual
	// Maybe: it depends on what pipewise cannot see: changed files, files in
	// the repository, unknown variables, earlier failures.
	Maybe
)

func (v Verdict) String() string {
	switch v {
	case Skipped:
		return "skipped"
	case Runs:
		return "runs"
	case Manual:
		return "manual"
	default:
		return "maybe"
	}
}

// JobResult holds a job's verdict for each event.
type JobResult struct {
	Job      *config.Job
	Verdicts [3]Verdict
	// Reasons say, for each event, what decided the verdict.
	Reasons [3]Reason
}

// On returns the job's verdict for ev.
func (r JobResult) On(ev Event) Verdict { return r.Verdicts[ev] }

// Result is the evaluation of a whole pipeline.
type Result struct {
	// Workflow says whether the pipeline is created at all for each event:
	// Runs, Skipped or Maybe.
	Workflow [3]Verdict
	Jobs     []JobResult
	// Problems are expressions GitLab would reject.
	Problems []config.Problem
}

// Options describe the project around the configuration.
type Options struct {
	// DefaultBranch is the project's default branch, such as "main".
	DefaultBranch string
	// SkipBranchPipelinesForMRs is the project setting of the same name. Like
	// workflow: rules, it removes the default `only: [branches, tags]` of jobs
	// that have neither rules nor only. It is not in the configuration, so
	// Unknown is the safe value when the project has not been asked.
	SkipBranchPipelinesForMRs Truth
}

// Evaluate decides, for every job of p, whether it runs on a merge request
// targeting the default branch, on a push to it, and on a tag, and records
// what decided each verdict.
func Evaluate(p *config.Pipeline, opts Options) *Result {
	hasWorkflowRules := p.Workflow != nil && len(p.Workflow.Rules) > 0
	e := &evaluator{
		exprs:           map[string]*Expr{},
		reported:        map[string]bool{},
		defaultsRemoved: or(truth(hasWorkflowRules), opts.SkipBranchPipelinesForMRs),
		removedBy:       "skip_branch_pipelines_for_mrs",
	}
	if hasWorkflowRules {
		e.removedBy = "workflow: rules"
	}
	res := &Result{}
	var workflowReasons [3]Reason
	for _, ev := range Events {
		res.Workflow[ev], workflowReasons[ev] = e.workflow(p.Workflow, NewContext(ev, opts.DefaultBranch))
		workflowReasons[ev].Workflow = true
	}
	for _, j := range p.Jobs {
		jr := JobResult{Job: j}
		for _, ev := range Events {
			v, why := e.job(j, NewContext(ev, opts.DefaultBranch))
			jr.Verdicts[ev], jr.Reasons[ev] = gate(res.Workflow[ev], v, workflowReasons[ev], why)
		}
		res.Jobs = append(res.Jobs, jr)
	}
	res.Problems = e.problems
	return res
}

// gate applies the workflow verdict to a job's: no pipeline, no job. The
// reason is the one that decided.
func gate(workflow, job Verdict, workflowWhy, jobWhy Reason) (Verdict, Reason) {
	switch {
	case workflow == Skipped:
		return Skipped, workflowWhy
	case job == Skipped:
		return Skipped, jobWhy
	case workflow == Maybe:
		return Maybe, workflowWhy
	default:
		return job, jobWhy
	}
}

type evaluator struct {
	exprs    map[string]*Expr
	reported map[string]bool
	problems []config.Problem
	// defaultsRemoved says whether jobs without rules or only lose the
	// default `only: [branches, tags]` (Entry::Processable#compose!), and
	// removedBy what removes it.
	defaultsRemoved Truth
	removedBy       string
	// notes are what pipewise cannot see, noted while evaluating a clause.
	notes []string
}

// note records something unknown the clause being evaluated depends on.
func (e *evaluator) note(what ...string) { e.notes = append(e.notes, what...) }

// settle drops the notes taken since mark when t does not depend on them.
func (e *evaluator) settle(mark int, t Truth) Truth {
	if t != Unknown {
		e.notes = e.notes[:mark]
	}
	return t
}

// take returns the notes taken since mark, without duplicates, and forgets
// them.
func (e *evaluator) take(mark int) []string {
	out := merge(e.notes[mark:])
	e.notes = e.notes[:mark]
	return out
}

// merge returns the items of lists in order, without duplicates.
func merge(lists ...[]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range lists {
		for _, s := range l {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

func (e *evaluator) report(job string, line int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	key := job + "\x00" + msg
	if e.reported[key] {
		return
	}
	e.reported[key] = true
	e.problems = append(e.problems, config.Problem{Kind: config.KindParseError, Job: job, Line: line, Message: msg})
}

// recorder notes the variables an expression reads without knowing them.
type recorder struct {
	*Context
	read []string
}

func (r *recorder) Lookup(name string) (string, VarState) {
	v, s := r.Context.Lookup(name)
	if s == UnknownValue || s == DefinedUnknown {
		r.read = append(r.read, "$"+name)
	}
	return v, s
}

// cond evaluates an expression. One GitLab cannot read counts as false, as
// GitLab's own evaluation does, and is reported.
func (e *evaluator) cond(src, job string, line int, ctx *Context) Truth {
	x, ok := e.exprs[src]
	if !ok {
		var err error
		x, err = ParseExpr(src)
		if err != nil {
			e.report(job, line, "invalid expression %q: %v", src, err)
			return False
		}
		e.exprs[src] = x
	}
	env := &recorder{Context: ctx}
	t, err := x.Eval(env)
	if err != nil {
		e.report(job, line, "expression %q cannot be evaluated: %v", src, err)
	}
	if t == Unknown {
		if len(env.read) == 0 {
			// Only inputs and the like are left to be unknown.
			env.read = []string{"inputs"}
		}
		e.note(env.read...)
	}
	return t
}

func (e *evaluator) workflow(w *config.Workflow, ctx *Context) (Verdict, Reason) {
	if w == nil || len(w.Rules) == 0 {
		return Runs, Reason{}
	}
	return e.rules(w.Rules, 0, "always", "", 0, ctx)
}

func (e *evaluator) job(j *config.Job, ctx *Context) (Verdict, Reason) {
	if j.HasRules {
		return e.rules(j.Rules, 0, j.When, j.Name, j.Line, ctx)
	}
	return e.onlyExcept(j, ctx)
}

// rules returns the verdict of the first matching rule from index i on. When
// a rule may or may not match, the verdict holds only if that rule and the
// rest of the list agree; otherwise it is Maybe. Either way, that rule is
// the reason.
func (e *evaluator) rules(rules []config.Rule, i int, jobWhen, job string, line int, ctx *Context) (Verdict, Reason) {
	if i == len(rules) {
		return Skipped, Reason{Source: SourceNoRule, Sure: true}
	}
	r := rules[i]
	mark := len(e.notes)
	matched := e.settle(mark, e.ruleMatches(r, job, line, ctx))
	why := Reason{Source: SourceRule, Index: i + 1, Clause: ruleClause(r), Sure: matched != Unknown}
	why.Unknowns = e.take(mark)
	switch matched {
	case True:
		here := ruleVerdict(r, jobWhen)
		if here == Maybe {
			why.Unknowns = append(why.Unknowns, "earlier failures")
		}
		return here, why
	case False:
		return e.rules(rules, i+1, jobWhen, job, line, ctx)
	default:
		here := ruleVerdict(r, jobWhen)
		rest, _ := e.rules(rules, i+1, jobWhen, job, line, ctx)
		if here == rest {
			return here, why
		}
		return Maybe, why
	}
}

func (e *evaluator) ruleMatches(r config.Rule, job string, line int, ctx *Context) Truth {
	if r.Reference != "" {
		e.note("!reference " + r.Reference)
		return Unknown
	}
	t := True
	if r.If != "" {
		t = and(t, e.cond(r.If, job, line, ctx))
	}
	if r.Changes {
		c := rulesChanges(r.ChangesCompareTo, ctx)
		if c == Unknown {
			e.note("changed files")
		}
		t = and(t, c)
	}
	if r.Exists {
		e.note("files in the repository")
		t = and(t, Unknown)
	}
	return t
}

// rulesChanges: without a push to compare with, as in a tag pipeline,
// `rules:changes` is always true unless compare_to gives it a reference.
func rulesChanges(compareTo bool, ctx *Context) Truth {
	if ctx.Event == Tag && !compareTo {
		return True
	}
	return Unknown
}

func ruleVerdict(r config.Rule, jobWhen string) Verdict {
	if r.Reference != "" {
		return Maybe
	}
	if r.When != "" {
		return whenVerdict(r.When)
	}
	return whenVerdict(jobWhen)
}

func whenVerdict(when string) Verdict {
	switch when {
	case "", "on_success", "always", "delayed":
		return Runs
	case "manual":
		return Manual
	case "never":
		return Skipped
	default:
		// on_failure runs only after a failure.
		return Maybe
	}
}

// defaultOnly is what a job without rules or only gets, unless
// workflow: rules or the project setting removes it.
var defaultOnly = &config.Filter{Refs: []string{"branches", "tags"}}

func (e *evaluator) onlyExcept(j *config.Job, ctx *Context) (Verdict, Reason) {
	if j.Only != nil {
		v, why := e.included(j.Only, j, ctx)
		why.Source, why.Clause = SourceOnlyExcept, filterClause(j.Only, j.Except)
		return v, why
	}
	kept := "GitLab's default only: [branches, tags]"
	if j.Except != nil {
		kept += "; " + filterClause(nil, j.Except)
	}
	switch e.defaultsRemoved {
	case True:
		v, why := e.included(nil, j, ctx)
		why.Source, why.Clause = SourceDefault, "GitLab's default only is removed by "+e.removedBy
		if j.Except != nil {
			why.Clause += "; " + filterClause(nil, j.Except)
		}
		return v, why
	case False:
		v, why := e.included(defaultOnly, j, ctx)
		why.Source, why.Clause = SourceDefault, kept
		return v, why
	default:
		k, why := e.included(defaultOnly, j, ctx)
		removed, removedWhy := e.included(nil, j, ctx)
		why.Source, why.Clause = SourceDefault, kept
		if k == removed {
			return k, why
		}
		// Either branch may apply: what either depends on counts.
		why.Sure = false
		why.Unknowns = merge(why.Unknowns, removedWhy.Unknowns, []string{"skip_branch_pipelines_for_mrs"})
		return Maybe, why
	}
}

// included follows GitLab's Seed::Build#included?: every key of only must
// match, and no key of except may. A nil only has no keys. The reason has
// no source or clause yet: the caller knows which filter it was.
func (e *evaluator) included(only *config.Filter, j *config.Job, ctx *Context) (Verdict, Reason) {
	mark := len(e.notes)
	in := True
	if only != nil {
		for _, t := range e.policies(only, j, ctx) {
			in = and(in, t)
		}
	}
	if j.Except != nil {
		for _, t := range e.policies(j.Except, j, ctx) {
			in = and(in, not(t))
		}
	}
	e.settle(mark, in)
	why := Reason{Sure: in != Unknown, Unknowns: e.take(mark)}
	w := whenVerdict(j.When)
	switch in {
	case True:
		if w == Maybe {
			why.Unknowns = append(why.Unknowns, "earlier failures")
		}
		return w, why
	case False:
		return Skipped, why
	default:
		if w == Skipped {
			return Skipped, why
		}
		return Maybe, why
	}
}

// policies returns one truth per key of an only/except filter.
func (e *evaluator) policies(f *config.Filter, j *config.Job, ctx *Context) []Truth {
	var out []Truth
	if len(f.Refs) > 0 {
		mark := len(e.notes)
		t := False
		for _, ref := range f.Refs {
			t = or(t, refMatches(ref, ctx, e))
		}
		out = append(out, e.settle(mark, t))
	}
	if len(f.Variables) > 0 {
		mark := len(e.notes)
		t := False
		for _, v := range f.Variables {
			t = or(t, e.cond(v, j.Name, j.Line, ctx))
		}
		out = append(out, e.settle(mark, t))
	}
	if f.Changes {
		// Policy::Changes is true when the pipeline has no modified paths, as
		// a tag pipeline does.
		t := Unknown
		if ctx.Event == Tag {
			t = True
		} else {
			e.note("changed files")
		}
		out = append(out, t)
	}
	return out
}

// sources maps a pipeline's source to the names only/except accept for it:
// the source without `_event`, and its plural.
var sources = map[Event][]string{
	MergeRequest:  {"merge_request", "merge_requests"},
	DefaultBranch: {"push", "pushes"},
	Tag:           {"push", "pushes"},
}

// refMatches follows GitLab's Policy::Refs for one pattern, and notes in e
// what it cannot know.
func refMatches(pattern string, ctx *Context, e *evaluator) Truth {
	name, path, scoped := strings.Cut(pattern, "@")
	t := patternMatches(name, ctx)
	if t == Unknown {
		e.note("the tag's name")
	}
	if scoped && path != "" {
		// pattern@group/project: the project path is not known here.
		if t != False {
			e.note("the project path")
		}
		t = and(t, Unknown)
	}
	return t
}

func patternMatches(pattern string, ctx *Context) Truth {
	switch {
	case pattern == "tags" && ctx.Event == Tag,
		pattern == "branches" && ctx.Event == DefaultBranch:
		return True
	}
	for _, s := range sources[ctx.Event] {
		if pattern == s {
			return True
		}
	}
	switch ctx.Event {
	case DefaultBranch:
		if re, ok := compilePattern(pattern); ok {
			return truth(re.MatchString(ctx.DefaultBranch))
		}
		return truth(pattern == ctx.DefaultBranch)
	case Tag:
		// GitLab compares any other pattern with the tag's name, which is
		// not known: names and regular expressions are unknown. Keywords
		// are taken as never matching, though GitLab would match one
		// against a tag named after it.
		if keywords[pattern] {
			return False
		}
		return Unknown
	default:
		// A merge request pipeline is neither a branch nor a tag pipeline:
		// names and patterns never match it.
		return False
	}
}

var keywords = map[string]bool{
	"api": true, "branches": true, "chat": true, "external": true,
	"external_pull_requests": true, "merge_requests": true, "pipelines": true,
	"pushes": true, "schedules": true, "tags": true, "triggers": true, "web": true,
}

func and(a, b Truth) Truth {
	switch {
	case a == False || b == False:
		return False
	case a == True && b == True:
		return True
	default:
		return Unknown
	}
}

func or(a, b Truth) Truth {
	switch {
	case a == True || b == True:
		return True
	case a == False && b == False:
		return False
	default:
		return Unknown
	}
}

func not(t Truth) Truth {
	switch t {
	case True:
		return False
	case False:
		return True
	default:
		return Unknown
	}
}
