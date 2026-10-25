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
// targeting the default branch, on a push to it, and on a tag.
func Evaluate(p *config.Pipeline, opts Options) *Result {
	hasWorkflowRules := p.Workflow != nil && len(p.Workflow.Rules) > 0
	e := &evaluator{
		exprs:           map[string]*Expr{},
		reported:        map[string]bool{},
		defaultsRemoved: or(truth(hasWorkflowRules), opts.SkipBranchPipelinesForMRs),
	}
	res := &Result{}
	for _, ev := range Events {
		res.Workflow[ev] = e.workflow(p.Workflow, NewContext(ev, opts.DefaultBranch))
	}
	for _, j := range p.Jobs {
		jr := JobResult{Job: j}
		for _, ev := range Events {
			jr.Verdicts[ev] = gate(res.Workflow[ev], e.job(j, NewContext(ev, opts.DefaultBranch)))
		}
		res.Jobs = append(res.Jobs, jr)
	}
	res.Problems = e.problems
	return res
}

// gate applies the workflow verdict to a job's: no pipeline, no job.
func gate(workflow, job Verdict) Verdict {
	switch {
	case workflow == Skipped || job == Skipped:
		return Skipped
	case workflow == Maybe:
		return Maybe
	default:
		return job
	}
}

type evaluator struct {
	exprs    map[string]*Expr
	reported map[string]bool
	problems []config.Problem
	// defaultsRemoved says whether jobs without rules or only lose the
	// default `only: [branches, tags]` (Entry::Processable#compose!).
	defaultsRemoved Truth
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
	t, err := x.Eval(ctx)
	if err != nil {
		e.report(job, line, "expression %q cannot be evaluated: %v", src, err)
	}
	return t
}

func (e *evaluator) workflow(w *config.Workflow, ctx *Context) Verdict {
	if w == nil || len(w.Rules) == 0 {
		return Runs
	}
	return e.rules(w.Rules, 0, "always", "", 0, ctx)
}

func (e *evaluator) job(j *config.Job, ctx *Context) Verdict {
	if j.HasRules {
		return e.rules(j.Rules, 0, j.When, j.Name, j.Line, ctx)
	}
	return e.onlyExcept(j, ctx)
}

// rules returns the verdict of the first matching rule from index i on. When
// a rule may or may not match, the verdict holds only if that rule and the
// rest of the list agree; otherwise it is Maybe.
func (e *evaluator) rules(rules []config.Rule, i int, jobWhen, job string, line int, ctx *Context) Verdict {
	if i == len(rules) {
		return Skipped
	}
	r := rules[i]
	switch e.ruleMatches(r, job, line, ctx) {
	case True:
		return ruleVerdict(r, jobWhen)
	case False:
		return e.rules(rules, i+1, jobWhen, job, line, ctx)
	default:
		here, rest := ruleVerdict(r, jobWhen), e.rules(rules, i+1, jobWhen, job, line, ctx)
		if here == rest {
			return here
		}
		return Maybe
	}
}

func (e *evaluator) ruleMatches(r config.Rule, job string, line int, ctx *Context) Truth {
	if r.Reference != "" {
		return Unknown
	}
	t := True
	if r.If != "" {
		t = and(t, e.cond(r.If, job, line, ctx))
	}
	if r.Changes {
		t = and(t, rulesChanges(r.ChangesCompareTo, ctx))
	}
	if r.Exists {
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

func (e *evaluator) onlyExcept(j *config.Job, ctx *Context) Verdict {
	if j.Only != nil {
		return e.included(j.Only, j, ctx)
	}
	switch e.defaultsRemoved {
	case True:
		return e.included(nil, j, ctx)
	case False:
		return e.included(defaultOnly, j, ctx)
	default:
		kept, removed := e.included(defaultOnly, j, ctx), e.included(nil, j, ctx)
		if kept == removed {
			return kept
		}
		return Maybe
	}
}

// included follows GitLab's Seed::Build#included?: every key of only must
// match, and no key of except may. A nil only has no keys.
func (e *evaluator) included(only *config.Filter, j *config.Job, ctx *Context) Verdict {
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
	w := whenVerdict(j.When)
	switch in {
	case True:
		return w
	case False:
		return Skipped
	default:
		if w == Skipped {
			return Skipped
		}
		return Maybe
	}
}

// policies returns one truth per key of an only/except filter.
func (e *evaluator) policies(f *config.Filter, j *config.Job, ctx *Context) []Truth {
	var out []Truth
	if len(f.Refs) > 0 {
		t := False
		for _, ref := range f.Refs {
			t = or(t, refMatches(ref, ctx))
		}
		out = append(out, t)
	}
	if len(f.Variables) > 0 {
		t := False
		for _, v := range f.Variables {
			t = or(t, e.cond(v, j.Name, j.Line, ctx))
		}
		out = append(out, t)
	}
	if f.Changes {
		// Policy::Changes is true when the pipeline has no modified paths, as
		// a tag pipeline does.
		t := Unknown
		if ctx.Event == Tag {
			t = True
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

// refMatches follows GitLab's Policy::Refs for one pattern.
func refMatches(pattern string, ctx *Context) Truth {
	name, path, scoped := strings.Cut(pattern, "@")
	t := patternMatches(name, ctx)
	if scoped && path != "" {
		// pattern@group/project: the project path is not known here.
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
