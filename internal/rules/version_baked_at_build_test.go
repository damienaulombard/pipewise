package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damienaulombard/pipewise/internal/eval"
)

func TestVersionBakedAtBuild(t *testing.T) {
	const tail = ", so the tag pipeline builds an artifact that no merge request tested."
	tests := []struct {
		fixture string
		job     string
		line    int
		// message is empty when nothing is reported.
		message string
	}{
		{"direct", "image-build", 2, `Build job "image-build" reads CI_COMMIT_TAG in its script` + tail},
		{"indirect", "package", 7, `Build job "package" reads CI_COMMIT_TAG through variable VERSION` + tail},
		{"job-variables", "kaniko", 2, `Build job "kaniko" reads CI_COMMIT_TAG through variable APP_VERSION` + tail},
		{"image", "build", 2, `Build job "build" reads CI_COMMIT_TAG in its image` + tail},
		{"build-and-deploy", "release", 2, `Build job "release" reads CI_COMMIT_TAG in its script` + tail},
		{"before-script", "image-build", 2, `Build job "image-build" reads CI_COMMIT_TAG in its script` + tail},
		{fixture: "deploy"},
		{fixture: "override"},
		{fixture: "not-the-tag"},
		{fixture: "trigger"},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rules", "version-baked-at-build", tt.fixture+".gitlab-ci.yml"))
			if err != nil {
				t.Fatal(err)
			}
			got := only(Analyze(data, eval.Options{DefaultBranch: "main"}).Findings, "version-baked-at-build")
			if tt.message == "" {
				if len(got) != 0 {
					t.Fatalf("got %+v, want nothing", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("got %d findings %+v, want 1", len(got), got)
			}
			f := got[0]
			if f.Severity != Warning || f.Job != tt.job || f.Line != tt.line || f.Message != tt.message || f.Explanation == "" {
				t.Errorf("got %+v\nwant job %q line %d message %q", f, tt.job, tt.line, tt.message)
			}
			if !strings.HasPrefix(f.Suggestion, tt.job+":\n") || !strings.Contains(f.Suggestion, "$CI_COMMIT_SHA") {
				t.Errorf("suggestion = %q", f.Suggestion)
			}
		})
	}
}

func TestIsBuildJob(t *testing.T) {
	for _, tt := range []struct {
		stage  string
		script string
		want   bool
	}{
		{"test", "docker build .", true},
		{"test", "DOCKER_BUILDKIT=1 docker build -t x .", true},
		{"test", "docker buildx build --push .", true},
		{"test", "/kaniko/executor --context .", true},
		{"test", "buildah bud -t x .", true},
		{"test", "buildah build -t x .", true},
		{"test", "podman build .", true},
		{"build", "make", true},
		{"test", "docker builder prune", false},
		{"test", "docker push x", false},
		{"builds", "make", false},
	} {
		if got := isBuildJob(tt.stage, []string{tt.script}); got != tt.want {
			t.Errorf("isBuildJob(%q, %q) = %v, want %v", tt.stage, tt.script, got, tt.want)
		}
	}
}
