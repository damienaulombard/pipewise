# Contributing to pipewise

Notes for anyone changing the code, human or coding agent. What pipewise does
and why is in the [README](README.md); the planned work is on the
[milestones](https://github.com/damienaulombard/pipewise/milestones).

## Scope

pipewise reads a GitLab CI configuration, works out for each job whether it
runs on three events, and reports the jobs that run more often than they need
to. It is static analysis only: no service, no network access except
`pipewise fetch`.

The three simulated events, with the predefined variables they set
(`internal/eval/context.go`):

| Event | Set | Undefined |
| --- | --- | --- |
| merge request | `CI_PIPELINE_SOURCE=merge_request_event`, `CI_MERGE_REQUEST_IID`, `CI_MERGE_REQUEST_TARGET_BRANCH_NAME` | `CI_COMMIT_BRANCH`, `CI_COMMIT_TAG` |
| push to the default branch | `CI_PIPELINE_SOURCE=push`, `CI_COMMIT_BRANCH=<default>` | `CI_COMMIT_TAG`, `CI_MERGE_REQUEST_*` |
| tag | `CI_PIPELINE_SOURCE=push`, `CI_COMMIT_TAG` | `CI_COMMIT_BRANCH`, `CI_MERGE_REQUEST_*` |

Each job gets a verdict per event: `runs`, `skipped`, `manual` or `maybe`.
`maybe` covers what the configuration alone cannot decide (`changes:`,
`exists:`, a variable pipewise cannot know) and never counts towards
`redundant-job`: pipewise only reports what it can prove.

Handled without failing: `trigger:` jobs and child pipelines are ignored;
unresolved `include:` and `extends` parents defined in an included file are
reported, and the analysis goes on with the rest.

## Layout

```
cmd/pipewise/       CLI (cobra): lint, explain, fetch, version
internal/config/    YAML → typed model; extends, anchors, !reference
internal/eval/      rules evaluator: events, if: expressions, verdicts
internal/rules/     one rule per file; rule.go holds Rule, Finding, All() and Analyze
internal/report/    text and JSON output
internal/gitlab/    minimal API client for fetch (merged YAML)
testdata/           fixtures and golden files
```

Dependencies: `gopkg.in/yaml.v3` and `github.com/spf13/cobra`. Add another
only with a reason stated in the pull request.

## Public interface

The JSON reports (`lint --format json`, `explain --format json`) and the rule
ids are what other tools build on. Rule ids are stable and kebab-case. None of
them changes without a major version; until v1.0, a change is called out in
the release notes.

## Adding a rule

1. A file in `internal/rules/` implementing `Rule`, registered in `All()`.
2. A message written for a developer who has not read the README: one sentence
   for what was found, one for the consequence, and a concrete YAML fix.
3. Table-driven tests with a fixture per case, including the cases that must
   not be reported.
4. The rule in the README table, and in `testdata/report/all-rules.*` when it
   changes the report.

## Tests

- Table-driven, fixtures in `testdata/`.
- Golden files for the reports; regenerate with
  `go test ./internal/report -update` and review the diff.
- `internal/eval` stays at **100 % coverage**, enforced by `make cover-eval`
  in `make check` and in CI: every verdict depends on it.
- An invalid configuration never panics: it becomes a finding and the
  analysis continues.

## Before a pull request

Go 1.26+ and golangci-lint v2.

```sh
make fmt
make check   # tidy check, lint, tests, coverage of internal/eval
```

## Conventions

- Everything in English: code, comments, CLI messages, docs, issues, pull
  requests.
- [Conventional Commits](https://www.conventionalcommits.org) with a scope:
  `feat(eval): …`, `fix(config): …`, `docs(readme): …`, `chore(repo): …`.
- One pull request per issue, squash-merged.
- No tool attribution in commits, pull requests or issues: no
  `Co-Authored-By` trailer, no "Generated with" line.
- CI runs on pull requests only, and the release workflow builds and publishes
  on a tag without testing again: the project follows its own rules.
