// Package actions parses a GitHub Actions workflow into its own model:
// triggers, jobs, steps and the reusable workflows jobs call. It does not
// evaluate `${{ }}` expressions or decide whether a job runs.
package actions

import "github.com/damienaulombard/pipewise/internal/config"

// KindUnresolvedWorkflow is a reusable workflow pipewise did not read: one in
// another repository, which it never fetches, or a local one it could not
// read. The jobs it defines are not analysed.
const KindUnresolvedWorkflow = "unresolved-workflow"

// Workflow is one workflow file.
type Workflow struct {
	// Name is the `name:` of the workflow, empty when not set.
	Name string
	// Triggers are the events of `on:`, in file order.
	Triggers []Trigger
	// Jobs are the jobs of `jobs:`, in file order, including the ones with
	// problems, as far as they could be read.
	Jobs []*Job
	// Problems lists what could not be read, here and in the local
	// reusable workflows the jobs call. Parsing never stops at the first.
	Problems []config.Problem
}

// Trigger is one event of `on:`.
type Trigger struct {
	Event string
	// Line is the line of the event's name.
	Line int
	// Branches, BranchesIgnore, Tags and TagsIgnore are the filter patterns
	// as written, nil when the filter is absent.
	Branches       []string
	BranchesIgnore []string
	Tags           []string
	TagsIgnore     []string
	// Paths records that `paths:` or `paths-ignore:` is set: whether the
	// event starts the workflow then depends on the files of a commit.
	Paths bool
}

// Job is one entry of `jobs:`.
type Job struct {
	// Name is the job id, the key under `jobs:`.
	Name string
	// Line is the line of the job id.
	Line  int
	Needs []string
	// If is the job-level `if:` as written, empty when absent.
	If    string
	Steps []Step
	// Uses is the reusable workflow the job calls, nil for a job that runs
	// steps.
	Uses *Call
}

// Step is one entry of a job's `steps:`.
type Step struct {
	Line int
	// Name is the `name:` of the step, empty when not set.
	Name string
	// Uses is the action reference as written (`owner/repo@ref`,
	// `./path`, `docker://image`), empty for a `run:` step. Actions are
	// never fetched.
	Uses string
	// Run is true for a `run:` step.
	Run bool
}

// Call is the `uses:` of a job calling a reusable workflow.
type Call struct {
	// Ref is the reference as written.
	Ref string
	// Line is the line of the `uses:` keyword.
	Line int
	// Local is true for a workflow of the same repository (`./…`).
	Local bool
	// Workflow is the called workflow once read, nil when it was not.
	Workflow *Workflow
}
