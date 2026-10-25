// Package config parses a GitLab CI configuration into a typed model and
// resolves `extends`, `default` and YAML anchors before evaluation.
package config

// Pipeline is a CI configuration as GitLab sees it once inheritance is
// applied: jobs come out with their `extends` parents and `default` merged in.
type Pipeline struct {
	// Workflow is nil when the configuration has no `workflow:` keyword.
	Workflow *Workflow
	// Jobs are the visible jobs, in file order. Hidden jobs (`.name`) only
	// serve as `extends` parents and are not listed.
	Jobs []*Job
	// Defaults is what jobs inherit from `default:` and the legacy top-level
	// keywords, with the line it is written on.
	Defaults Defaults
	// Problems lists what could not be read. Parsing never stops at the
	// first one: whatever could be read is still in Workflow and Jobs.
	Problems []Problem
}

// Workflow holds the pipeline-level `workflow: rules`.
type Workflow struct {
	Rules []Rule
}

// Job is one visible job after inheritance.
type Job struct {
	Name string
	// Line is the line of the job's key in the source file.
	Line  int
	Stage string
	// When is the job-level `when`, empty when not set.
	When string
	// HasRules tells `rules: []` apart from no `rules` at all.
	HasRules bool
	Rules    []Rule
	// Only and Except are nil when the keyword is absent.
	Only   *Filter
	Except *Filter
	// Image is nil when no image applies, neither the job's nor a default.
	Image *Image
	// Artifacts is nil when the job declares none.
	Artifacts *Artifacts
	// Interruptible is nil when not set.
	Interruptible *bool
	// Trigger marks a job that starts a downstream pipeline.
	Trigger bool
	// Extends lists the parents the job declared, before resolution.
	Extends []string
}

// Rule is one entry of `rules:` or `workflow: rules:`.
type Rule struct {
	// If is the `if:` expression, empty when absent.
	If   string
	When string
	// Changes and Exists only record that the clause is present: their
	// outcome depends on the files of a commit, which pipewise does not see.
	Changes bool
	// ChangesCompareTo records `changes: compare_to:`, which makes the clause
	// compare against a ref even in pipelines without a push, such as tags.
	ChangesCompareTo bool
	Exists           bool
	AllowFailure     *bool
	// Reference is set for a `!reference [...]` entry that was not resolved,
	// as the dotted path it points to. The rule's content is then unknown.
	Reference string
}

// Filter is the content of `only:` or `except:`.
type Filter struct {
	Refs      []string
	Variables []string
	// Changes records that a `changes:` clause is present.
	Changes bool
}

// Defaults are the values a job takes when it does not set them itself.
type Defaults struct {
	// Image is nil when there is no default image.
	Image *Image
	// ImageLine is the line of `default:`, or of the top-level `image:`.
	ImageLine int
	// Artifacts is nil when `default:` has no artifacts.
	Artifacts     *Artifacts
	ArtifactsLine int
}

// Image is the container image a job runs in.
type Image struct {
	Name string
	// Inherited is true when the job took it from Defaults.
	Inherited bool
}

// Artifacts is the part of `artifacts:` the rules look at.
type Artifacts struct {
	// ExpireIn is empty when `expire_in` is not set.
	ExpireIn string
	// Inherited is true when the job took them from Defaults.
	Inherited bool
}

// Problem kinds.
const (
	// KindParseError is a configuration that cannot be read as written.
	KindParseError = "parse-error"
	// KindUnresolvedInclude is an `include:` left in the configuration:
	// pipewise works on merged YAML and does not fetch included files.
	KindUnresolvedInclude = "unresolved-include"
	// KindUnresolvedReference is a `!reference` tag, not resolved yet.
	KindUnresolvedReference = "unresolved-reference"
)

// Problem is something in the configuration pipewise could not read.
type Problem struct {
	Kind string
	// Job is empty for a problem outside any job.
	Job     string
	Line    int
	Message string
}
