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
(default `https://gitlab.com`). Public projects need no token.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--format` | `text` | `text`, or `json` for tools |
| `--fail-on` | `error` | lowest severity that makes the command fail: `error`, `warning`, `info` or `none` |
| `--default-branch` | `main` | the project's default branch |
| `--skip-branch-pipelines-for-mrs` | `unknown` | the GitLab project setting of that name: `true`, `false` or `unknown` |

Exit codes: `0` when no finding reaches `--fail-on`, `1` when one does, `2` on
a usage or read error.

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

## Develop

Requires Go 1.26+ and [golangci-lint](https://golangci-lint.run) v2.

```sh
make build   # bin/pipewise
make test
make lint
make check   # tidy check, lint, tests and the coverage of internal/eval, as the CI runs them
```

## License

[Apache License 2.0](LICENSE)
