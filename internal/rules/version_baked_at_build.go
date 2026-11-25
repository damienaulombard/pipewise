package rules

import (
	"fmt"
	"regexp"
	"sort"
)

// versionBakedAtBuild reports a build job that reads CI_COMMIT_TAG, directly
// or through variables: the tag pipeline then builds something the merge
// request never tested. A build job runs an image or package build, or sits
// in the stage named build. A deploy job that uses the tag is not reported;
// a job that both builds and deploys is.
type versionBakedAtBuild struct{}

func (versionBakedAtBuild) ID() string { return "version-baked-at-build" }

var (
	buildCommand = regexp.MustCompile(`\b(docker( buildx)? build|buildah (bud|build)|podman build)\b|/kaniko/executor`)
	// variableRef matches $NAME and ${NAME}, the name in group 1 or 2.
	variableRef = regexp.MustCompile(`\$(?:\{([A-Za-z_][A-Za-z0-9_]*)\}|([A-Za-z_][A-Za-z0-9_]*))`)
)

func isBuildJob(stage string, script []string) bool {
	if stage == "build" {
		return true
	}
	for _, c := range script {
		if buildCommand.MatchString(c) {
			return true
		}
	}
	return false
}

// refs returns the variable names s reads.
func refs(s string) []string {
	var out []string
	for _, m := range variableRef.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1]+m[2])
	}
	return out
}

const tagVariable = "CI_COMMIT_TAG"

// tainted returns the variables whose value reads the tag, directly or
// through another tainted variable.
func tainted(vars map[string]string) map[string]bool {
	out := map[string]bool{tagVariable: true}
	for changed := true; changed; {
		changed = false
		for name, value := range vars {
			if out[name] {
				continue
			}
			for _, r := range refs(value) {
				if out[r] {
					out[name], changed = true, true
					break
				}
			}
		}
	}
	return out
}

func readsTag(texts []string) bool {
	for _, t := range texts {
		for _, r := range refs(t) {
			if r == tagVariable {
				return true
			}
		}
	}
	return false
}

func (r versionBakedAtBuild) Check(a *Analysis) []Finding {
	var out []Finding
	for _, j := range a.Pipeline.Jobs {
		if j.Trigger || !isBuildJob(j.Stage, j.Script) {
			continue
		}
		var image []string
		if j.Image != nil {
			image = []string{j.Image.Name}
		}
		vars := map[string]string{}
		for k, v := range a.Pipeline.Variables {
			vars[k] = v
		}
		for k, v := range j.Variables {
			vars[k] = v
		}
		bad := tainted(vars)
		// Variables that can carry the tag into the build: the job's own,
		// and those its script or image reads.
		var through []string
		used := map[string]bool{}
		for name := range j.Variables {
			used[name] = true
		}
		for _, t := range append(append([]string{}, j.Script...), image...) {
			for _, ref := range refs(t) {
				used[ref] = true
			}
		}
		for name := range used {
			if name != tagVariable && bad[name] {
				through = append(through, name)
			}
		}
		sort.Strings(through)

		var where string
		switch {
		case readsTag(j.Script):
			where = "in its script"
		case readsTag(image):
			where = "in its image"
		case len(through) > 0:
			where = "through variable " + through[0]
		default:
			continue
		}
		out = append(out, Finding{
			Rule:     r.ID(),
			Severity: Warning,
			Job:      j.Name,
			Line:     j.Line,
			Message: fmt.Sprintf("Build job %q reads CI_COMMIT_TAG %s, "+
				"so the tag pipeline builds an artifact that no merge request tested.", j.Name, where),
			Explanation: "The merge request pipeline tested an artifact built without the tag; the tag pipeline " +
				"builds a new one and ships it untested. Build once with the commit SHA, then on the tag only " +
				"retag or promote that artifact, and inject the version at deploy time.",
			Suggestion: fmt.Sprintf("%s:\n  # build with the commit SHA, never the tag\n  script:\n    - docker build -t $CI_REGISTRY_IMAGE:$CI_COMMIT_SHA .\n"+
				"retag:\n  stage: deploy\n  script:\n"+
				"    - docker buildx imagetools create -t $CI_REGISTRY_IMAGE:$CI_COMMIT_TAG $CI_REGISTRY_IMAGE:$CI_COMMIT_SHA\n"+
				"  rules:\n    - if: $CI_COMMIT_TAG\n", j.Name),
		})
	}
	return out
}
