package rules

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// backticked matches an inline code span, such as `no-cache`.
var backticked = regexp.MustCompile("`([^`]+)`")

// plannedRules returns the ids written in backticks in the bullets of the
// README `## Roadmap` section that announce rules: those whose bold label
// contains "more rules". Other bullets may name shipped rules for another
// reason, as the targets of --fix, and are not read.
func plannedRules(readme string) []string {
	var section []string
	in := false
	for line := range strings.SplitSeq(readme, "\n") {
		if strings.HasPrefix(line, "## ") {
			in = strings.TrimSpace(line) == "## Roadmap"
			continue
		}
		if in {
			section = append(section, line)
		}
	}

	// A bullet goes on over its indented lines; any other line ends it.
	var bullets []string
	open := false
	for _, line := range section {
		switch {
		case strings.HasPrefix(line, "- "):
			bullets = append(bullets, line)
			open = true
		case open && strings.HasPrefix(line, "  ") && strings.TrimSpace(line) != "":
			bullets[len(bullets)-1] += " " + strings.TrimSpace(line)
		default:
			open = false
		}
	}

	var ids []string
	for _, b := range bullets {
		label, _, ok := strings.Cut(strings.TrimPrefix(b, "- **"), "**")
		if !ok || !strings.Contains(label, "more rules") {
			continue
		}
		for _, m := range backticked.FindAllStringSubmatch(b, -1) {
			ids = append(ids, m[1])
		}
	}
	return ids
}

func TestPlannedRules(t *testing.T) {
	tests := []struct {
		name   string
		readme string
		want   []string
	}{
		{
			name: "rules bullet, continued on the next line",
			readme: "## Roadmap\n\n- **v0.4 — more rules**: `a-rule`,\n  `b-rule`.\n" +
				"- **v1.0 — other**: `c-rule`.\n",
			want: []string{"a-rule", "b-rule"},
		},
		{
			name:   "autofix bullet naming shipped rules is not read",
			readme: "## Roadmap\n\n- **v0.3 — autofix**: `--fix` for\n  `no-cache`.\n",
		},
		{
			name:   "outside the Roadmap section",
			readme: "## Rules\n\n- **more rules**: `a-rule`\n\n## Roadmap\n\nNothing planned.\n\n## Develop\n\n- **more rules**: `b-rule`\n",
		},
		{
			name:   "a paragraph after the bullet is not part of it",
			readme: "## Roadmap\n\n- **more rules**: `a-rule`.\n\nIdeas: `b-rule`.\n",
			want:   []string{"a-rule"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := plannedRules(tt.readme); !slices.Equal(got, tt.want) {
				t.Errorf("plannedRules = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestShippedRulesLeftTheRoadmap fails when a rule of All() is still listed
// as planned in the README Roadmap: shipping a rule takes it out of there
// (AGENTS.md, "Adding a rule").
func TestShippedRulesLeftTheRoadmap(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	planned := plannedRules(string(data))
	for _, r := range All() {
		if slices.Contains(planned, r.ID()) {
			t.Errorf("rule %s is shipped but still listed under \"more rules\" in the README Roadmap; remove it there", r.ID())
		}
	}
}
