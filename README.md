# pipewise

Find the GitLab CI jobs that run more often than they need to.

Teams often run the same build, lint and test jobs several times for the same
commit: on the merge request, again when it lands on `main`, and again when it
is tagged. pipewise reads the pipeline configuration, works out which jobs run
on each of those events, and tells you which ones are redundant and how to fix
them.

> Status: early development. Nothing below the `version` command works yet.

## Install

```sh
go install github.com/damienaulombard/pipewise/cmd/pipewise@latest
```

## Develop

Requires Go 1.26+ and [golangci-lint](https://golangci-lint.run) v2.

```sh
make build   # bin/pipewise
make test
make lint
make check   # tidy check + lint + test, as the CI runs them
```

## License

[Apache License 2.0](LICENSE)
