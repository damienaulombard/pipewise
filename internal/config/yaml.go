package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// reference is an unresolved `!reference [path...]` tag.
type reference struct {
	path []string
	line int
}

func (r reference) String() string { return strings.Join(r.path, ".") }

// toValue converts a YAML node into plain Go values: map[string]any, []any,
// scalars, and reference for `!reference` tags. Aliases are followed and
// merge keys (`<<`) applied, the way GitLab reads them: keys written in the
// mapping win over merged ones, and earlier merge sources win over later ones.
//
// Decoding straight into map[string]any is not enough: yaml.v3 then turns
// `!reference [.rules, mr]` into the plain list [".rules", "mr"] and the
// reference is silently lost.
func toValue(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.AliasNode:
		return toValue(n.Alias)
	case yaml.SequenceNode:
		if n.Tag == "!reference" {
			return toReference(n)
		}
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := toValue(c)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.MappingNode:
		return toMap(n)
	default:
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, fmt.Errorf("line %d: %w", n.Line, err)
		}
		return v, nil
	}
}

func toReference(n *yaml.Node) (any, error) {
	ref := reference{line: n.Line}
	for _, c := range n.Content {
		if c.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("line %d: !reference expects a list of keys", n.Line)
		}
		ref.path = append(ref.path, c.Value)
	}
	return ref, nil
}

func toMap(n *yaml.Node) (map[string]any, error) {
	out := make(map[string]any, len(n.Content)/2)
	var merged []map[string]any
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		if key.Tag == "!!merge" {
			sources, err := mergeSources(val)
			if err != nil {
				return nil, err
			}
			merged = append(merged, sources...)
			continue
		}
		v, err := toValue(val)
		if err != nil {
			return nil, err
		}
		out[key.Value] = v
	}
	for _, src := range merged {
		for k, v := range src {
			if _, set := out[k]; !set {
				out[k] = v
			}
		}
	}
	return out, nil
}

// mergeSources returns the mappings a `<<` key merges, in precedence order.
func mergeSources(n *yaml.Node) ([]map[string]any, error) {
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	switch n.Kind {
	case yaml.MappingNode:
		m, err := toMap(n)
		if err != nil {
			return nil, err
		}
		return []map[string]any{m}, nil
	case yaml.SequenceNode:
		var out []map[string]any
		for _, c := range n.Content {
			m, err := mergeSources(c)
			if err != nil {
				return nil, err
			}
			out = append(out, m...)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("line %d: a merge key (<<) must point to a mapping", n.Line)
	}
}
