// Package eval decides, for each job, whether it runs on a merge request,
// on the default branch and on a tag, by evaluating its `rules`,
// `only`/`except` and the pipeline's `workflow: rules`.
package eval
