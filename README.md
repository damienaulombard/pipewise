# pipewise

Find the GitLab CI jobs that run more often than they need to.

Teams often run the same build, lint and test jobs several times for the same
commit: on the merge request, again when it lands on `main`, and again when it
is tagged. pipewise reads the pipeline configuration, works out which jobs run
on each of those events, and tells you which ones are redundant and how to fix
them.

```
.gitlab-ci.yml:8: error redundant-job
  Job "lint" runs 2 times for the same commit: on the default branch and on the
  tag.
  why: With merged results pipelines and no direct pushes to the default branch,
       the merge request pipeline already tests the exact commit that lands on
       the default branch, and a tag only names that commit. Running the job
       again there cannot find anything new; it only costs runner time and
       delays the release.
  fix:
    lint:
      rules:
        - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
```

> Status: early development.

## Install

```sh
go install github.com/damienaulombard/pipewise/cmd/pipewise@latest
```

Or download an archive for Linux, macOS or Windows from the
[releases](https://github.com/damienaulombard/pipewise/releases), and check it
against `SHA256SUMS`.

## Use

```sh
pipewise lint .gitlab-ci.yml
pipewise lint --format json .gitlab-ci.yml
cat .gitlab-ci.yml | pipewise lint -
```

pipewise analyses the configuration as written. If it uses `include:`, analyse
the merged configuration instead, which `pipewise fetch` downloads from GitLab
with every include expanded:

```sh
pipewise fetch --project group/app | pipewise lint -
pipewise fetch --project group/app --ref v1.2.0 -o merged.yml
```

`fetch` reads the token from `GITLAB_TOKEN` and the instance from `GITLAB_URL`
(default `https://gitlab.com`, or your self-managed instance). Public projects
need no token. A private project needs a token with the `read_api` scope;
without one, GitLab answers 404, as if the project did not exist.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--format` | `text` | `text`, or `json` for tools |
| `--fail-on` | `error` | lowest severity that makes the command fail: `error`, `warning`, `info` or `none` |
| `--default-branch` | `main` | the project's default branch |
| `--skip-branch-pipelines-for-mrs` | `unknown` | the GitLab project setting of that name: `true`, `false` or `unknown` |

Exit codes: `0` when no finding reaches `--fail-on`, `1` when one does, `2` on
a usage or read error.

To see why a job runs, or does not, on each event, ask for it by name:

```sh
pipewise explain test                  # reads .gitlab-ci.yml
pipewise explain --format json test merged.yml
```

```
Job "test" (.gitlab-ci.yml:9)

merge request    runs     rule 1 (.gitlab-ci.yml:13) matched: if:
                          $CI_PIPELINE_SOURCE == "merge_request_event"
default branch   maybe    rule 2 (.gitlab-ci.yml:14) may match: if:
                          $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH, changes
                          (depends on changed files)
tag              skipped  rule 3 (.gitlab-ci.yml:16) matched: if:
                          $CI_COMMIT_TAG, when: never
```

`explain` takes the same `--format`, `--default-branch` and
`--skip-branch-pipelines-for-mrs` flags as `lint`, and exits with `2` when the
job does not exist.

## What it reports

| Rule | Severity | When |
| --- | --- | --- |
| `redundant-job` | error | a job surely runs on two or more of merge request, default branch and tag |
| `no-rules` | warning | a job has no `rules`, `only` or `except`, so GitLab's defaults decide where it runs |
| `missing-interruptible` | info | a job runs on merge requests without setting `interruptible` |
| `floating-image` | warning | an image uses `latest` or no tag |
| | info | an image is pinned by tag but not by digest |
| `artifacts-no-expiry` | info | `artifacts:` without `expire_in` |

Rule ids are stable: they are part of pipewise's interface, like the JSON
report.

pipewise only reports what it can prove from the configuration. What depends
on things it cannot see (changed files, project variables, the name of a tag)
is "maybe", and never counts towards `redundant-job`.

## Why these rules

CI observability tools measure durations, trends and flaky tests. None of
them reads the configuration to see that a job runs three times for one
commit, and proposes the fix. The rules follow from a few principles:

- **One stage validates.** With merged results pipelines and no direct pushes
  to the default branch, the merge request pipeline tests exactly the commit
  that lands on the default branch. Testing it again there, or on the tag,
  cannot reveal anything new.
- **A tag is a name, not new code.** The tag pipeline publishes and deploys;
  it does not validate again.
- **Build once, deploy many.** Building more than once is a known delivery
  anti-pattern. Rebuilding on the tag is acceptable only when the build is
  strictly reproducible (lockfiles, base images pinned by digest) and followed
  by a smoke test.
- **A smoke test checks the packaging, not the logic:** start the published
  image, check its health and version, send a request or two.
- **Each pipeline has one role:** the merge request validates, the default
  branch publishes (or does nothing), the tag names and deploys.

Further reading: GitLab's
[pipeline efficiency](https://docs.gitlab.com/ci/pipelines/pipeline_efficiency/)
guide, [Continuous Delivery](https://continuousdelivery.com) (Humble and
Farley), the anti-patterns of the AWS Well-Architected DevOps Guidance,
*Software Engineering at Google* (chapter 23), and the DORA reports on lead
time for changes.

## Roadmap

No dates: each step ships when it is ready. The issues of each step are on the
[milestones page](https://github.com/damienaulombard/pipewise/milestones).

- **v0.2 — explain**: `pipewise explain <job>` shows which rule decides
  whether a job runs, for each event.
- **v0.3 — autofix**: `--fix` prints a patch for the findings it can fix
  safely, starting with `missing-interruptible` and `artifacts-no-expiry`,
  then `redundant-job` once the project settings are checked.
- **v0.4 — more rules**: `e2e-everywhere`, `stages-without-needs`,
  `version-baked-at-build`, `no-cache`.
- **v1.0 — GitHub Actions**: the same analysis for GitHub Actions workflows,
  and a frozen JSON report and rule ids.

Ideas and use cases are welcome in the
[issues](https://github.com/damienaulombard/pipewise/issues).

## Develop

Requires Go 1.26+ and [golangci-lint](https://golangci-lint.run) v2.

How the code is organised and the conventions to follow are in
[AGENTS.md](AGENTS.md).

```sh
make build   # bin/pipewise
make test
make lint
make check   # tidy check, lint, tests and the coverage of internal/eval, as the CI runs them
```

## License

[Apache License 2.0](LICENSE)
