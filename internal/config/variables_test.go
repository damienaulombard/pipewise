package config

import (
	"reflect"
	"testing"
)

func TestParseVariables(t *testing.T) {
	p := Parse([]byte(`variables:
  GLOBAL: one
  DESCRIBED:
    value: two
    description: shown in the UI
  NUMBER: 3
  FLAG: true
  NOTHING:
  LIST: [a, b]
  NO_VALUE:
    description: no value
.base:
  variables:
    FROM_PARENT: $CI_COMMIT_TAG
build:
  extends: .base
  variables:
    OWN: ${GLOBAL}-x
none:
  script: [x]
`))
	wantGlobal := map[string]string{"GLOBAL": "one", "DESCRIBED": "two", "NUMBER": "3", "FLAG": "true"}
	if !reflect.DeepEqual(p.Variables, wantGlobal) {
		t.Errorf("global variables = %v, want %v", p.Variables, wantGlobal)
	}
	want := map[string]map[string]string{
		"build": {"FROM_PARENT": "$CI_COMMIT_TAG", "OWN": "${GLOBAL}-x"},
		"none":  nil,
	}
	for _, j := range p.Jobs {
		if !reflect.DeepEqual(j.Variables, want[j.Name]) {
			t.Errorf("%s: variables = %v, want %v", j.Name, j.Variables, want[j.Name])
		}
	}
}
