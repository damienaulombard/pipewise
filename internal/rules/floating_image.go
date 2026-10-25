package rules

import (
	"fmt"
	"strings"
)

// floatingImage reports images that can change without a commit: `latest`
// or no tag at all (warning), and a tag without a digest (info, since a
// tag can be pushed again). Images built from variables are skipped: their
// value is not known.
//
// The default image is reported once, on `default:`, naming the jobs that
// inherit it.
type floatingImage struct{}

func (floatingImage) ID() string { return "floating-image" }

func (r floatingImage) Check(a *Analysis) []Finding {
	var out []Finding
	d := a.Pipeline.Defaults
	if d.Image != nil {
		var users []string
		for _, j := range a.Pipeline.Jobs {
			if !j.Trigger && j.Image != nil && j.Image.Inherited {
				users = append(users, fmt.Sprintf("%q", j.Name))
			}
		}
		if f, ok := r.check(d.Image.Name); ok && len(users) > 0 {
			f.Line = d.ImageLine
			f.Message = fmt.Sprintf("The default image %q %s; %s.", d.Image.Name, f.Message, usedBy(users, "it"))
			f.Suggestion = "default:\n" + f.Suggestion
			out = append(out, f)
		}
	}
	for _, j := range a.Pipeline.Jobs {
		if j.Trigger || j.Image == nil || j.Image.Inherited {
			continue
		}
		if f, ok := r.check(j.Image.Name); ok {
			f.Job = j.Name
			f.Line = j.Line
			f.Message = fmt.Sprintf("Job %q runs in image %q, which %s.", j.Name, j.Image.Name, f.Message)
			f.Suggestion = j.Name + ":\n" + f.Suggestion
			out = append(out, f)
		}
	}
	return out
}

// check returns the finding for one image name, with Message holding only
// what is wrong with it, or false when the image is pinned or unknown.
func (r floatingImage) check(name string) (Finding, bool) {
	if name == "" || strings.Contains(name, "$") || strings.Contains(name, "@sha256:") {
		return Finding{}, false
	}
	repo, tag := splitTag(name)
	f := Finding{Rule: r.ID()}
	switch tag {
	case "":
		f.Severity = Warning
		f.Message = "has no tag, so GitLab pulls latest"
	case "latest":
		f.Severity = Warning
		f.Message = "uses the latest tag"
	default:
		f.Severity = Info
		f.Message = "is pinned by tag but not by digest"
	}
	if f.Severity == Warning {
		f.Explanation = "A floating image changes under the pipeline without any commit: " +
			"the same commit can pass today and fail tomorrow, and nothing can be rebuilt as it was."
		tag = "<version>"
	} else {
		f.Explanation = "A tag can be pushed again with other content. A digest pins the exact image, " +
			"so a rebuild uses the same bytes."
	}
	f.Explanation += fmt.Sprintf(" `docker buildx imagetools inspect %s:%s` prints the digest.", repo, tag)
	f.Suggestion = fmt.Sprintf("  image: %s:%s@sha256:<digest>\n", repo, tag)
	return f, true
}

// splitTag splits "registry:5000/group/app:1.2" into the repository and
// the tag; a colon before the last slash belongs to a registry port.
func splitTag(name string) (repo, tag string) {
	i := strings.LastIndex(name, ":")
	if i < 0 || i < strings.LastIndex(name, "/") {
		return name, ""
	}
	return name[:i], name[i+1:]
}
