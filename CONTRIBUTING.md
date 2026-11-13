# Contributing to pipewise

Thanks for your interest. pipewise finds the CI jobs that run more often than
they need to; contributions that make it find more, explain better, or run on
more pipelines are welcome.

## Before you write code

Open an issue first, or comment on an existing one, and say what you plan to
do. A short agreement on the approach saves a rewrite later. Typos and small
documentation fixes can go straight to a pull request.

The planned work is on the
[milestones](https://github.com/damienaulombard/pipewise/milestones); issues
there are a good place to start.

## Asking for a rule

pipewise reports what it can prove from the configuration. A good rule request
says:

- the CI configuration it would catch, as a minimal `.gitlab-ci.yml` extract;
- why it costs time or money: what runs, how often, for which commits;
- the fix you would expect pipewise to suggest;
- the cases it must not report.

## Reporting a bug

Include:

- the configuration, or the smallest extract that shows the problem;
- the command you ran and the pipewise version (`pipewise version`);
- what you expected, and what you got (the text or JSON output).

## Proposing a change

1. One pull request per issue, with `Closes #<issue>` in its description.
2. The pull request title follows
   [Conventional Commits](https://www.conventionalcommits.org) with a scope,
   such as `feat(eval): …` or `fix(config): …`. Pull requests are
   squash-merged, so that title becomes the commit on `main`.
3. Tests come with the change, as described in the Tests section of
   [AGENTS.md](AGENTS.md#tests).
4. Run `make fmt` and `make check` before asking for review. CI runs the same
   checks on the pull request.
5. Keep the change to what the issue asks; open another issue for anything
   else you notice on the way.

How the code is organised, the testing requirements and the full
[conventions](AGENTS.md#conventions), including the language to write in, are
in [AGENTS.md](AGENTS.md). Releases are cut by the maintainer: there is
nothing to do for them in a contribution.

## Licence

pipewise is licensed under the [Apache License 2.0](LICENSE). By contributing,
you agree that your contributions are licensed under it too.
