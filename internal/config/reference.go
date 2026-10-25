package config

import "strings"

type refState int

const (
	refPending refState = iota
	refResolving
	refDone
	refFailed
)

// referenceResolver replaces `!reference` tags with what they point to, the
// way GitLab does (lib/gitlab/ci/config/yaml/tags): when the YAML is loaded,
// so before `extends`; following mappings only; with no depth limit but
// circular chains rejected; and a missing or null value as an error.
//
// GitLab rejects the whole configuration on such an error. pipewise reports
// it, leaves that one reference unresolved, and keeps going.
type referenceResolver struct {
	p      *parser
	raw    map[string]any
	state  map[*reference]refState
	values map[*reference]any
	// current is the top-level key being resolved, named in problems.
	current string
}

// resolveReferences returns raw with every reference that can be resolved
// replaced by its value. Keys are walked in file order, so that a problem is
// always reported against the same key.
func (p *parser) resolveReferences(raw map[string]any, order []entry) map[string]any {
	r := &referenceResolver{
		p:      p,
		raw:    raw,
		state:  map[*reference]refState{},
		values: map[*reference]any{},
	}
	out := make(map[string]any, len(raw))
	for _, e := range order {
		r.current = e.name
		out[e.name] = r.deep(raw[e.name])
	}
	return out
}

func (r *referenceResolver) deep(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			out[k] = r.deep(item)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = r.deep(item)
		}
		return out
	case *reference:
		return r.resolve(x)
	default:
		return v
	}
}

func (r *referenceResolver) resolve(ref *reference) any {
	switch r.state[ref] {
	case refDone:
		return r.values[ref]
	case refFailed:
		return ref
	case refResolving:
		r.fail(ref, "is part of a circular chain")
		return ref
	}
	r.state[ref] = refResolving
	target, ok := r.dig(ref.path)
	if !ok {
		r.fail(ref, "could not be found")
		return ref
	}
	value := r.deep(target)
	if r.state[ref] == refFailed {
		// The chain came back to this reference.
		return ref
	}
	r.state[ref] = refDone
	r.values[ref] = value
	return value
}

// dig follows path through mappings of the configuration as written. A null
// or false value counts as missing, as in GitLab.
func (r *referenceResolver) dig(path []string) (any, bool) {
	var cur any = r.raw
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[key]; !ok {
			return nil, false
		}
	}
	if cur == nil || cur == false {
		return nil, false
	}
	return cur, true
}

func (r *referenceResolver) fail(ref *reference, why string) {
	r.state[ref] = refFailed
	r.p.problem(KindUnresolvedReference, r.current, ref.line,
		"!reference [%s] %s; what it stands for is treated as unknown",
		strings.Join(ref.path, ", "), why)
}
