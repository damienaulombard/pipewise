package rules

import (
	"strings"
	"testing"
)

// brief is the part of a finding these tests compare.
type brief struct {
	Severity Severity
	Job      string
	Line     int
	Message  string
}

func briefs(findings []Finding) []brief {
	var out []brief
	for _, f := range findings {
		out = append(out, brief{f.Severity, f.Job, f.Line, f.Message})
	}
	return out
}

func assertFindings(t *testing.T, yaml, rule string, want []brief) []Finding {
	t.Helper()
	got := only(analyze(yaml), rule)
	b := briefs(got)
	if len(b) != len(want) {
		t.Fatalf("%s: got %d findings %+v, want %+v", rule, len(b), b, want)
	}
	for i := range want {
		if b[i] != want[i] {
			t.Errorf("%s finding %d:\n got  %+v\n want %+v", rule, i, b[i], want[i])
		}
		if got[i].Explanation == "" || got[i].Suggestion == "" {
			t.Errorf("%s finding %d has no explanation or suggestion", rule, i)
		}
	}
	return got
}

func TestNoRules(t *testing.T) {
	got := assertFindings(t, `lint:
  script: [x]
with-rules:
  script: [x]
  rules: [{when: on_success}]
with-only:
  script: [x]
  only: [branches]
with-except:
  script: [x]
  except: [tags]
downstream:
  trigger: {project: group/other}
`, "no-rules", []brief{
		{Warning, "lint", 1, `Job "lint" has no rules, only or except, so GitLab's defaults decide where it runs.`},
	})
	if !strings.Contains(got[0].Suggestion, `merge_request_event`) {
		t.Errorf("suggestion = %q", got[0].Suggestion)
	}
}

func TestMissingInterruptible(t *testing.T) {
	mr := `
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
`
	got := assertFindings(t, `unit:
  script: [x]`+mr+`
explicit-true:
  interruptible: true
  script: [x]`+mr+`
explicit-false:
  interruptible: false
  script: [x]`+mr+`
tag-only:
  script: [x]
  rules:
    - if: $CI_COMMIT_TAG
maybe-on-mr:
  script: [x]
  rules:
    - changes: [go.mod]
downstream:
  trigger: {project: group/other}`+mr, "missing-interruptible", []brief{
		{Info, "unit", 1, `Job "unit" runs on merge requests but does not set interruptible.`},
	})
	if got[0].Suggestion != "unit:\n  interruptible: true\n" {
		t.Errorf("suggestion = %q", got[0].Suggestion)
	}

	// interruptible inherited from default: counts as set.
	assertFindings(t, "default:\n  interruptible: true\nunit:\n  script: [x]"+mr, "missing-interruptible", nil)

	// Without rules but under workflow: rules, a job runs on merge requests.
	assertFindings(t, `workflow:
  rules: [{when: always}]
lint:
  script: [x]
`, "missing-interruptible", []brief{
		{Info, "lint", 3, `Job "lint" runs on merge requests but does not set interruptible.`},
	})
}

func TestFloatingImage(t *testing.T) {
	tests := []struct {
		image    string
		severity Severity
		what     string // empty when the image is fine
		suggest  string
	}{
		{"node", Warning, "has no tag, so GitLab pulls latest", "node:<version>@sha256:<digest>"},
		{"node:latest", Warning, "uses the latest tag", "node:<version>@sha256:<digest>"},
		{"node:22", Info, "is pinned by tag but not by digest", "node:22@sha256:<digest>"},
		{"registry:5000/group/app", Warning, "has no tag, so GitLab pulls latest", "registry:5000/group/app:<version>@sha256:<digest>"},
		{"registry:5000/group/app:1.2", Info, "is pinned by tag but not by digest", "registry:5000/group/app:1.2@sha256:<digest>"},
		{"node:22@sha256:0123456789abcdef", 0, "", ""},
		{"node@sha256:0123456789abcdef", 0, "", ""},
		{"$CI_REGISTRY_IMAGE:$CI_COMMIT_SHA", 0, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			yaml := "build:\n  image: \"" + tt.image + "\"\n  script: [x]\n"
			if tt.what == "" {
				assertFindings(t, yaml, "floating-image", nil)
				return
			}
			got := assertFindings(t, yaml, "floating-image", []brief{
				{tt.severity, "build", 1, `Job "build" runs in image "` + tt.image + `", which ` + tt.what + "."},
			})
			if want := "build:\n  image: " + tt.suggest + "\n"; got[0].Suggestion != want {
				t.Errorf("suggestion = %q, want %q", got[0].Suggestion, want)
			}
			if !strings.Contains(got[0].Explanation, "docker buildx imagetools inspect") {
				t.Errorf("explanation = %q", got[0].Explanation)
			}
		})
	}

	// An image mapping without a name says nothing about the image.
	assertFindings(t, "build:\n  image:\n    entrypoint: [\"\"]\n  script: [x]\n", "floating-image", nil)
}

func TestFloatingDefaultImage(t *testing.T) {
	got := assertFindings(t, `default:
  image: node:latest
a:
  script: [x]
b:
  image: golang:1.26
  script: [x]
c:
  script: [x]
downstream:
  trigger: {project: group/other}
`, "floating-image", []brief{
		// Once on default:, naming only the jobs that inherit it.
		{Warning, "", 1, `The default image "node:latest" uses the latest tag; jobs "a" and "c" use it.`},
		{Info, "b", 5, `Job "b" runs in image "golang:1.26", which is pinned by tag but not by digest.`},
	})
	if got[0].Suggestion != "default:\n  image: node:<version>@sha256:<digest>\n" {
		t.Errorf("suggestion = %q", got[0].Suggestion)
	}

	// The legacy top-level image, used by one job.
	assertFindings(t, "stages: [test]\nimage: ruby\nlint:\n  script: [x]\n", "floating-image", []brief{
		{Warning, "", 2, `The default image "ruby" has no tag, so GitLab pulls latest; job "lint" uses it.`},
	})

	// A default image every job overrides is not reported.
	assertFindings(t, "default:\n  image: node\na:\n  image: node:22@sha256:0123\n", "floating-image", nil)

	// A pinned default image is fine.
	assertFindings(t, "default:\n  image: node:22@sha256:0123\na:\n  script: [x]\n", "floating-image", nil)
}

func TestArtifactsNoExpiry(t *testing.T) {
	got := assertFindings(t, `build:
  script: [x]
  artifacts:
    paths: [bin/]
kept:
  script: [x]
  artifacts:
    paths: [bin/]
    expire_in: 1 week
downstream:
  trigger: {project: group/other}
  artifacts:
    paths: [bin/]
`, "artifacts-no-expiry", []brief{
		{Info, "build", 1, `Job "build" keeps its artifacts with no expire_in.`},
	})
	if got[0].Suggestion != "build:\n  artifacts:\n    expire_in: 1 week\n" {
		t.Errorf("suggestion = %q", got[0].Suggestion)
	}
}

func TestArtifactsNoExpiryFromDefault(t *testing.T) {
	got := assertFindings(t, `stages: [build]
default:
  artifacts:
    paths: [out/]
a:
  script: [x]
b:
  script: [x]
c:
  script: [x]
  artifacts:
    paths: [bin/]
    expire_in: 1 day
`, "artifacts-no-expiry", []brief{
		{Info, "", 2, `The default artifacts have no expire_in; jobs "a" and "b" use them.`},
	})
	if !strings.HasPrefix(got[0].Suggestion, "default:\n") {
		t.Errorf("suggestion = %q", got[0].Suggestion)
	}

	// Default artifacts with expire_in, or that no job inherits: nothing.
	assertFindings(t, "default:\n  artifacts:\n    expire_in: 1 day\na:\n  script: [x]\n", "artifacts-no-expiry", nil)
	assertFindings(t, "default:\n  artifacts:\n    paths: [out/]\na:\n  artifacts:\n    expire_in: 1 day\n", "artifacts-no-expiry", nil)
}
