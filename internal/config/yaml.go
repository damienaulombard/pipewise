package config

import (
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// reference is a `!reference [path...]` tag. It is a pointer so that each
// tag in the file has an identity, which cycle detection relies on.
type reference struct {
	path []string
	line int
}

func (r *reference) String() string { return strings.Join(r.path, ".") }

// reader converts YAML nodes into plain Go values: map[string]any, []any,
// scalars, and *reference for `!reference` tags. Aliases are followed and
// merge keys (`<<`) applied, the way GitLab reads them: keys written in the
// mapping win over merged ones, and earlier merge sources win over later ones.
//
// Decoding straight into map[string]any is not enough: yaml.v3 then turns
// `!reference [.rules, mr]` into the plain list [".rules", "mr"] and the
// reference is silently lost.
type reader struct {
	// open holds the mappings and lists being read, from the root down to
	// the current node. An alias of one of them has no finite value: yaml.v3
	// accepts `.base: &base` then `<<: *base` inside it.
	open map[*yaml.Node]bool

	// produced counts the values read (mappings, lists, scalars), aliased
	// those read while an alias is followed, and aliasDepth the aliases
	// being followed. Aliases of aliases grow exponentially: produce stops
	// the reading the way yaml.v3 guards its own decoding, which does not
	// apply here since the file is decoded into yaml.Node.
	produced, aliased, aliasDepth int
}

// errAliasExpansion is returned once aliases produce too many values.
var errAliasExpansion = errors.New("the configuration expands its YAML aliases into too many values")

// produce counts one value read, and fails with yaml.v3's rule
// (go.yaml.in/yaml/v3 v3.0.6, decode.go, allowedAliasRatio): once more than
// 100 values come from aliases and more than 1 000 are read, the share read
// through aliases may not exceed allowedAliasRatio.
func (r *reader) produce() error {
	r.produced++
	if r.aliasDepth > 0 {
		r.aliased++
	}
	if r.aliased > 100 && r.produced > 1000 && float64(r.aliased)/float64(r.produced) > allowedAliasRatio(r.produced) {
		return errAliasExpansion
	}
	return nil
}

const (
	aliasRatioRangeLow  = 400_000
	aliasRatioRangeHigh = 4_000_000
)

// allowedAliasRatio is the share of values read through aliases allowed
// once produced values are read: 99 % up to 400 000, falling linearly to
// 10 % at 4 000 000, and 10 % beyond.
func allowedAliasRatio(produced int) float64 {
	switch {
	case produced <= aliasRatioRangeLow:
		return 0.99
	case produced >= aliasRatioRangeHigh:
		return 0.10
	default:
		return 0.99 - 0.89*float64(produced-aliasRatioRangeLow)/float64(aliasRatioRangeHigh-aliasRatioRangeLow)
	}
}

// toMap converts a mapping node; see reader.
func toMap(n *yaml.Node) (map[string]any, error) {
	r := &reader{open: map[*yaml.Node]bool{}}
	return r.mapping(n)
}

// follow returns the node an alias points to, or an error when that node is
// one of those being read.
func (r *reader) follow(alias *yaml.Node) (*yaml.Node, error) {
	if r.open[alias.Alias] {
		return nil, fmt.Errorf("line %d: an alias refers to the mapping or list it is written in", alias.Line)
	}
	return alias.Alias, nil
}

func (r *reader) value(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.AliasNode:
		target, err := r.follow(n)
		if err != nil {
			return nil, err
		}
		r.aliasDepth++
		defer func() { r.aliasDepth-- }()
		return r.value(target)
	case yaml.MappingNode:
		return r.mapping(n)
	}
	if err := r.produce(); err != nil {
		return nil, err
	}
	switch n.Kind {
	case yaml.SequenceNode:
		if n.Tag == "!reference" {
			return toReference(n)
		}
		r.open[n] = true
		defer delete(r.open, n)
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := r.value(c)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	default:
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, fmt.Errorf("line %d: %w", n.Line, err)
		}
		return v, nil
	}
}

func toReference(n *yaml.Node) (any, error) {
	ref := &reference{line: n.Line}
	for _, c := range n.Content {
		// GitLab accepts only strings: a key written 1 or true is rejected.
		if c.Kind != yaml.ScalarNode || c.ShortTag() != "!!str" {
			return nil, fmt.Errorf("line %d: !reference expects a list of keys", n.Line)
		}
		ref.path = append(ref.path, c.Value)
	}
	if len(ref.path) == 0 {
		return nil, fmt.Errorf("line %d: !reference expects a list of keys", n.Line)
	}
	return ref, nil
}

// lineKey and keyLinesKey are kept in every mapping read from the file:
// the line of the mapping, and the line of each of its keys. Merged keys
// keep the line they are written on. Both start with a NUL so that they
// cannot be taken for keywords or job names; isHidden tells them apart.
const (
	lineKey     = "\x00line"
	keyLinesKey = "\x00keys"
)

func isHidden(key string) bool { return strings.HasPrefix(key, "\x00") }

// mapLine returns the line a mapping read by toMap starts on, or 0.
func mapLine(m map[string]any) int {
	l, _ := m[lineKey].(int)
	return l
}

// keyLine returns the line key is written on in a mapping read by toMap,
// or 0.
func keyLine(m map[string]any, key string) int {
	lines, _ := m[keyLinesKey].(map[string]any)
	l, _ := lines[key].(int)
	return l
}

func (r *reader) mapping(n *yaml.Node) (map[string]any, error) {
	if err := r.produce(); err != nil {
		return nil, err
	}
	r.open[n] = true
	defer delete(r.open, n)
	out := make(map[string]any, len(n.Content)/2+2)
	keyLines := map[string]any{}
	var merged []map[string]any
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		if key.Tag == "!!merge" {
			sources, err := r.mergeSources(val)
			if err != nil {
				return nil, err
			}
			merged = append(merged, sources...)
			continue
		}
		v, err := r.value(val)
		if err != nil {
			return nil, err
		}
		out[key.Value] = v
		keyLines[key.Value] = key.Line
	}
	for _, src := range merged {
		for k, v := range src {
			if isHidden(k) {
				continue
			}
			if _, set := out[k]; !set {
				out[k] = v
				if l := keyLine(src, k); l > 0 {
					keyLines[k] = l
				}
			}
		}
	}
	// The mapping starts at its first key: an anchor on the line above
	// (`tag: &rule` then `if: ...`) is not where it is written.
	line := n.Line
	if len(n.Content) > 0 {
		line = n.Content[0].Line
	}
	out[lineKey] = line
	out[keyLinesKey] = keyLines
	return out, nil
}

// mergeSources returns the mappings a `<<` key merges, in precedence order.
func (r *reader) mergeSources(n *yaml.Node) ([]map[string]any, error) {
	if n.Kind == yaml.AliasNode {
		target, err := r.follow(n)
		if err != nil {
			return nil, err
		}
		r.aliasDepth++
		defer func() { r.aliasDepth-- }()
		n = target
	}
	switch n.Kind {
	case yaml.MappingNode:
		m, err := r.mapping(n)
		if err != nil {
			return nil, err
		}
		return []map[string]any{m}, nil
	case yaml.SequenceNode:
		r.open[n] = true
		defer delete(r.open, n)
		var out []map[string]any
		for _, c := range n.Content {
			m, err := r.mergeSources(c)
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
