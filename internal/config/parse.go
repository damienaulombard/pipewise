package config

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// globalKeywords are the top-level keys that are not jobs.
var globalKeywords = map[string]bool{
	"after_script":  true,
	"before_script": true,
	"cache":         true,
	"default":       true,
	"image":         true,
	"include":       true,
	"services":      true,
	"stages":        true,
	"types":         true,
	"variables":     true,
	"workflow":      true,
}

// Parse reads a GitLab CI configuration. It never fails: what cannot be read
// is reported in Pipeline.Problems and the rest is still returned.
func Parse(data []byte) *Pipeline {
	p := &parser{pipeline: &Pipeline{}}
	p.parse(data)
	return p.pipeline
}

type parser struct {
	pipeline *Pipeline
}

func (p *parser) problem(kind, job string, line int, format string, args ...any) {
	p.pipeline.Problems = append(p.pipeline.Problems, Problem{
		Kind:    kind,
		Job:     job,
		Line:    line,
		Message: fmt.Sprintf(format, args...),
	})
}

type entry struct {
	name string
	line int
}

func (p *parser) parse(data []byte) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		p.problem(KindParseError, "", 0, "invalid YAML: %v", err)
		return
	}
	if len(doc.Content) == 0 {
		return
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		p.problem(KindParseError, "", root.Line, "the configuration must be a mapping of keywords and jobs")
		return
	}
	raw, err := toMap(root)
	if err != nil {
		p.problem(KindParseError, "", 0, "%v", err)
		return
	}

	// Written keys in file order, then the ones a top-level `<<` brought in,
	// located at that `<<` line: GitLab runs merged jobs like written ones.
	var entries []entry
	seen := map[string]bool{}
	mergeLine := 0
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		if key.Tag == "!!merge" {
			mergeLine = key.Line
			continue
		}
		entries = append(entries, entry{name: key.Value, line: key.Line})
		seen[key.Value] = true
	}
	var merged []string
	for name := range raw {
		if !seen[name] {
			merged = append(merged, name)
		}
	}
	sort.Strings(merged)
	for _, name := range merged {
		entries = append(entries, entry{name: name, line: mergeLine})
	}

	raw = p.resolveReferences(raw, entries)

	if _, ok := raw["include"]; ok {
		p.problem(KindUnresolvedInclude, "", lineOf(entries, "include"),
			"include: is not resolved; analyse the merged configuration (pipewise fetch) to see the included jobs")
	}
	p.parseWorkflow(raw["workflow"], lineOf(entries, "workflow"))

	r := newResolver(raw)
	defaults := defaultsOf(raw)
	p.parseDefaults(raw, defaults, entries)
	for _, e := range entries {
		if globalKeywords[e.name] || strings.HasPrefix(e.name, ".") {
			continue
		}
		if _, ok := raw[e.name].(map[string]any); !ok {
			p.problem(KindParseError, e.name, e.line, "job %q must be a mapping", e.name)
			continue
		}
		resolved, err := r.resolve(e.name)
		if err != nil {
			p.problem(KindParseError, e.name, e.line, "%v", err)
			continue
		}
		m, inherited := applyDefaults(resolved, defaults)
		p.pipeline.Jobs = append(p.pipeline.Jobs, p.toJob(e, m, inherited))
	}
}

func lineOf(entries []entry, name string) int {
	for _, e := range entries {
		if e.name == name {
			return e.line
		}
	}
	return 0
}

func (p *parser) parseWorkflow(v any, line int) {
	if v == nil {
		return
	}
	m, ok := v.(map[string]any)
	if !ok {
		p.problem(KindParseError, "", line, "workflow: must be a mapping")
		return
	}
	w := &Workflow{}
	if rules, ok := m["rules"]; ok {
		w.Rules = p.toRules(rules, "", line)
	}
	p.pipeline.Workflow = w
}

// parseDefaults records the default image and artifacts and where they are
// written: on `default:`, or for the image on the legacy top-level `image:`.
func (p *parser) parseDefaults(raw, defaults map[string]any, entries []entry) {
	d := &p.pipeline.Defaults
	defaultLine := lineOf(entries, "default")
	if v, ok := defaults["image"]; ok {
		d.ImageLine = defaultLine
		if dm, isMap := raw["default"].(map[string]any); !isMap || dm["image"] == nil {
			d.ImageLine = lineOf(entries, "image")
		}
		d.Image = p.toImage(v, entry{name: "default", line: d.ImageLine})
	}
	if a, ok := defaults["artifacts"].(map[string]any); ok {
		d.Artifacts = toArtifacts(a)
		d.ArtifactsLine = defaultLine
	}
}

func toArtifacts(a map[string]any) *Artifacts {
	out := &Artifacts{}
	if s, ok := a["expire_in"].(string); ok {
		out.ExpireIn = s
	}
	return out
}

func (p *parser) toJob(e entry, m map[string]any, inherited map[string]bool) *Job {
	j := &Job{Name: e.name, Line: e.line, Stage: "test"}
	if s, ok := m["stage"].(string); ok {
		j.Stage = s
	}
	if s, ok := m["when"].(string); ok {
		j.When = s
	}
	if v, ok := m["rules"]; ok {
		j.HasRules = true
		j.Rules = p.toRules(v, e.name, e.line)
	}
	j.Only = p.toFilter(m["only"], "only", e)
	j.Except = p.toFilter(m["except"], "except", e)
	j.Image = p.toImage(m["image"], e)
	if j.Image != nil {
		j.Image.Inherited = inherited["image"]
	}
	if a, ok := m["artifacts"].(map[string]any); ok {
		j.Artifacts = toArtifacts(a)
		j.Artifacts.Inherited = inherited["artifacts"]
	}
	if b, ok := m["interruptible"].(bool); ok {
		j.Interruptible = &b
	}
	_, j.Trigger = m["trigger"]
	j.Extends = stringList(m["extends"])
	return j
}

func (p *parser) toRules(v any, job string, line int) []Rule {
	list, ok := v.([]any)
	if !ok {
		// A reference still here could not be resolved, and was reported.
		if ref, isRef := v.(*reference); isRef {
			return []Rule{{Reference: ref.String()}}
		}
		p.problem(KindParseError, job, line, "rules: must be a list")
		return nil
	}
	var out []Rule
	for _, item := range list {
		switch it := item.(type) {
		case map[string]any:
			out = append(out, toRule(it))
		case *reference:
			out = append(out, Rule{Reference: it.String()})
		case []any:
			// GitLab flattens nested lists in rules; they come from references.
			out = append(out, p.toRules(it, job, line)...)
		default:
			p.problem(KindParseError, job, line, "each rule must be a mapping")
		}
	}
	return out
}

func toRule(m map[string]any) Rule {
	r := Rule{}
	if s, ok := m["if"].(string); ok {
		r.If = s
	}
	if s, ok := m["when"].(string); ok {
		r.When = s
	}
	_, r.Changes = m["changes"]
	if c, ok := m["changes"].(map[string]any); ok {
		_, r.ChangesCompareTo = c["compare_to"]
	}
	_, r.Exists = m["exists"]
	if b, ok := m["allow_failure"].(bool); ok {
		r.AllowFailure = &b
	}
	return r
}

func (p *parser) toFilter(v any, keyword string, e entry) *Filter {
	switch f := v.(type) {
	case nil:
		return nil
	case string:
		return &Filter{Refs: []string{f}}
	case []any:
		return &Filter{Refs: stringList(f)}
	case map[string]any:
		_, changes := f["changes"]
		return &Filter{Refs: stringList(f["refs"]), Variables: stringList(f["variables"]), Changes: changes}
	default:
		p.problem(KindParseError, e.name, e.line, "%s: must be a list or a mapping", keyword)
		return nil
	}
}

func (p *parser) toImage(v any, e entry) *Image {
	switch img := v.(type) {
	case nil:
		return nil
	case string:
		return &Image{Name: img}
	case map[string]any:
		name, _ := img["name"].(string)
		return &Image{Name: name}
	case *reference:
		// Unresolved and already reported: the image is unknown.
		return nil
	default:
		p.problem(KindParseError, e.name, e.line, "image: must be a string or a mapping")
		return nil
	}
}

func stringList(v any) []string {
	switch l := v.(type) {
	case string:
		return []string{l}
	case []any:
		var out []string
		for _, item := range l {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
