package config

import (
	"reflect"
	"testing"
)

func TestParseStages(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []string
		line int
	}{
		{"default", "a:\n  script: [x]\n", []string{".pre", "build", "test", "deploy", ".post"}, 0},
		{"declared", "x: 1\nstages: [lint, test]\na:\n  script: [x]\n", []string{".pre", "lint", "test", ".post"}, 2},
		{"legacy types", "types: [build, ship]\n", []string{".pre", "build", "ship", ".post"}, 1},
		{"stages wins over types", "types: [a]\nstages: [b]\n", []string{".pre", "b", ".post"}, 2},
		{".pre and .post written", "stages: [.pre, build, .post]\n", []string{".pre", "build", ".post"}, 1},
		{"not a list of strings", "stages: {a: 1}\n", []string{".pre", "build", "test", "deploy", ".post"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Parse([]byte(tt.yaml))
			if !reflect.DeepEqual(p.Stages, tt.want) || p.StagesLine != tt.line {
				t.Errorf("stages = %q line %d, want %q line %d", p.Stages, p.StagesLine, tt.want, tt.line)
			}
		})
	}
	if p := Parse([]byte("stages: {a: 1}\n")); len(p.Problems) != 1 || p.Problems[0].Kind != KindParseError {
		t.Errorf("problems = %+v, want one parse error", p.Problems)
	}
}

func TestParseNeedsAndDependencies(t *testing.T) {
	p := Parse([]byte(`.base:
  needs: [build]
none:
  script: [x]
own:
  needs:
    - build
    - job: lint
      artifacts: false
empty:
  needs: []
inherited:
  extends: .base
deps:
  dependencies: [build, lint]
no-deps:
  dependencies: []
`))
	type got struct {
		Needs        bool
		Dependencies []string
	}
	want := map[string]got{
		"none":      {},
		"own":       {Needs: true},
		"empty":     {Needs: true},
		"inherited": {Needs: true},
		"deps":      {Dependencies: []string{"build", "lint"}},
		"no-deps":   {Dependencies: []string{}},
	}
	if len(p.Jobs) != len(want) {
		t.Fatalf("jobs = %d, want %d", len(p.Jobs), len(want))
	}
	for _, j := range p.Jobs {
		if g := (got{j.Needs, j.Dependencies}); !reflect.DeepEqual(g, want[j.Name]) {
			t.Errorf("%s: got %+v, want %+v", j.Name, g, want[j.Name])
		}
	}
}
