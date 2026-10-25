package config

import (
	"fmt"
	"strings"
)

// resolver applies `extends` the way GitLab does: parents are merged in the
// order they are listed, then the job's own keys on top. Mappings are merged
// key by key, at any depth; any other value (lists included, so `rules`) is
// replaced as a whole.
type resolver struct {
	raw      map[string]any
	resolved map[string]map[string]any
	visiting []string
}

func newResolver(raw map[string]any) *resolver {
	return &resolver{raw: raw, resolved: map[string]map[string]any{}}
}

func (r *resolver) resolve(name string) (map[string]any, error) {
	if m, ok := r.resolved[name]; ok {
		return m, nil
	}
	for i, v := range r.visiting {
		if v == name {
			cycle := append(append([]string{}, r.visiting[i:]...), name)
			return nil, fmt.Errorf("extends cycle: %s", strings.Join(cycle, " -> "))
		}
	}
	own, ok := r.raw[name].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("extends %q: no such job or hidden job", name)
	}

	r.visiting = append(r.visiting, name)
	defer func() { r.visiting = r.visiting[:len(r.visiting)-1] }()

	out := map[string]any{}
	for _, parent := range stringList(own["extends"]) {
		pm, err := r.resolve(parent)
		if err != nil {
			return nil, err
		}
		out = deepMerge(out, pm)
	}
	out = deepMerge(out, own)
	// extends describes a job's own parents; it is not inherited.
	delete(out, "extends")
	if e, ok := own["extends"]; ok {
		out["extends"] = e
	}
	r.resolved[name] = out
	return out, nil
}

// deepMerge returns base overlaid with over, without modifying either.
func deepMerge(base, over map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		bm, baseIsMap := out[k].(map[string]any)
		om, overIsMap := v.(map[string]any)
		if baseIsMap && overIsMap {
			out[k] = deepMerge(bm, om)
			continue
		}
		out[k] = v
	}
	return out
}

// defaultKeywords are the keys a job takes from `default:` when it does not
// set them itself.
var defaultKeywords = []string{
	"after_script", "artifacts", "before_script", "cache", "hooks", "id_tokens",
	"image", "interruptible", "retry", "services", "tags", "timeout",
}

// legacyGlobals are top-level keywords that act as defaults, from before
// `default:` existed.
var legacyGlobals = []string{"after_script", "before_script", "cache", "image", "services"}

// defaultsOf returns the values jobs inherit, `default:` first, then the
// legacy top-level keywords for what `default:` leaves unset.
func defaultsOf(raw map[string]any) map[string]any {
	out := map[string]any{}
	if d, ok := raw["default"].(map[string]any); ok {
		for _, k := range defaultKeywords {
			if v, set := d[k]; set {
				out[k] = v
			}
		}
	}
	for _, k := range legacyGlobals {
		if _, set := out[k]; set {
			continue
		}
		if v, set := raw[k]; set {
			out[k] = v
		}
	}
	return out
}

// applyDefaults fills the keys a job leaves unset from defaults, honouring
// `inherit: default:` (true, false, or the list of keywords to take). It
// also returns the keys it filled.
func applyDefaults(job, defaults map[string]any) (map[string]any, map[string]bool) {
	allowed := func(string) bool { return true }
	if inherit, ok := job["inherit"].(map[string]any); ok {
		switch d := inherit["default"].(type) {
		case bool:
			if !d {
				return job, nil
			}
		case []any:
			keep := map[string]bool{}
			for _, k := range stringList(d) {
				keep[k] = true
			}
			allowed = func(k string) bool { return keep[k] }
		}
	}
	out := make(map[string]any, len(job)+len(defaults))
	for k, v := range job {
		out[k] = v
	}
	inherited := map[string]bool{}
	for k, v := range defaults {
		if _, set := out[k]; !set && allowed(k) {
			out[k] = v
			inherited[k] = true
		}
	}
	return out, inherited
}
