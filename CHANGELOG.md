# Changelog

## [0.3.0](https://github.com/damienaulombard/pipewise/compare/v0.2.0...v0.3.0) (2026-11-16)


### Features

* **cli:** add pipewise explain to show what decides each verdict ([#43](https://github.com/damienaulombard/pipewise/issues/43)) ([769f903](https://github.com/damienaulombard/pipewise/commit/769f903784a6d6cb0a030181de5878c2bf1ae1b1)), closes [#26](https://github.com/damienaulombard/pipewise/issues/26)
* **cli:** add the lint command ([#16](https://github.com/damienaulombard/pipewise/issues/16)) ([1e18496](https://github.com/damienaulombard/pipewise/commit/1e18496dd55a61525234e1efcfd9fa64ecae0726)), closes [#14](https://github.com/damienaulombard/pipewise/issues/14)
* **cli:** fetch the merged configuration from GitLab ([#23](https://github.com/damienaulombard/pipewise/issues/23)) ([bfe279b](https://github.com/damienaulombard/pipewise/commit/bfe279b851e4cc6e5932b046f4ebeee645c69282)), closes [#17](https://github.com/damienaulombard/pipewise/issues/17)
* **config:** parse the CI configuration into a typed model ([#5](https://github.com/damienaulombard/pipewise/issues/5)) ([dcbddcb](https://github.com/damienaulombard/pipewise/commit/dcbddcbb13ff9a47d50869b417a50cf320245526)), closes [#2](https://github.com/damienaulombard/pipewise/issues/2)
* **config:** resolve !reference tags ([#8](https://github.com/damienaulombard/pipewise/issues/8)) ([fdae180](https://github.com/damienaulombard/pipewise/commit/fdae180f93407264cc9add26a24d4c00397b1242)), closes [#6](https://github.com/damienaulombard/pipewise/issues/6)
* **eval:** decide whether each job runs on MR, default branch and tag ([#7](https://github.com/damienaulombard/pipewise/issues/7)) ([79fce3b](https://github.com/damienaulombard/pipewise/commit/79fce3b17cfee8e7d02725ac133cf2a76c7e7339)), closes [#3](https://github.com/damienaulombard/pipewise/issues/3)
* **explain:** show where each deciding rule is written ([#45](https://github.com/damienaulombard/pipewise/issues/45)) ([6637f57](https://github.com/damienaulombard/pipewise/commit/6637f57163e6a74ced122eac8c798787a85578cc)), closes [#41](https://github.com/damienaulombard/pipewise/issues/41)
* **report:** render findings as text and JSON ([#15](https://github.com/damienaulombard/pipewise/issues/15)) ([7f42abe](https://github.com/damienaulombard/pipewise/commit/7f42abed6f4348b51ec7efccef1a4e197a9149bd)), closes [#13](https://github.com/damienaulombard/pipewise/issues/13)
* **rules:** add the four remaining v0 rules ([#12](https://github.com/damienaulombard/pipewise/issues/12)) ([0815f99](https://github.com/damienaulombard/pipewise/commit/0815f99fef013f9e982651ccc281b2b0eca81fd4)), closes [#10](https://github.com/damienaulombard/pipewise/issues/10)
* **rules:** report jobs that run more than once per commit ([#11](https://github.com/damienaulombard/pipewise/issues/11)) ([d5b9277](https://github.com/damienaulombard/pipewise/commit/d5b92773e3bec9019c92ae747321d3bbbfd08a2a)), closes [#9](https://github.com/damienaulombard/pipewise/issues/9)


### Bug Fixes

* **config:** read the configuration after a spec: header ([#20](https://github.com/damienaulombard/pipewise/issues/20)) ([784482d](https://github.com/damienaulombard/pipewise/commit/784482d0c674be2aa074a9b939d0a6abd2a04062)), closes [#19](https://github.com/damienaulombard/pipewise/issues/19)
* **config:** treat a missing extends parent as included ([#22](https://github.com/damienaulombard/pipewise/issues/22)) ([fc30785](https://github.com/damienaulombard/pipewise/commit/fc30785dbd38390acb38462ce18ef0cabbfe521c)), closes [#21](https://github.com/damienaulombard/pipewise/issues/21)
* **eval:** list what either default-only branch depends on ([#44](https://github.com/damienaulombard/pipewise/issues/44)) ([4d57eea](https://github.com/damienaulombard/pipewise/commit/4d57eea8b7b5c17377d94f1c308c854b7f5ab10a)), closes [#42](https://github.com/damienaulombard/pipewise/issues/42)

## [0.2.0](https://github.com/damienaulombard/pipewise/compare/v0.1.0...v0.2.0) (2026-11-16)


### Public interface

* New JSON report: `pipewise explain --format json` (`schema`, `file`, `job`, `line`, and per event `event`, `verdict` and `decided_by`). Its field names are public interface, like those of `lint --format json`.
* Added to `decided_by` during the milestone: `line` (where the deciding clause is written, `0` for GitLab's default `only` or when no rule matched) and `from` (the top-level key holding it when it is not the job itself: a template reached through `extends`, an anchor or `!reference`); `except_line` and `except_from`, the same for the job's `except`.


### Features

* **cli:** add pipewise explain to show what decides each verdict ([#43](https://github.com/damienaulombard/pipewise/issues/43)) ([769f903](https://github.com/damienaulombard/pipewise/commit/769f903784a6d6cb0a030181de5878c2bf1ae1b1)), closes [#26](https://github.com/damienaulombard/pipewise/issues/26)
* **explain:** show where each deciding rule is written ([#45](https://github.com/damienaulombard/pipewise/issues/45)) ([6637f57](https://github.com/damienaulombard/pipewise/commit/6637f57163e6a74ced122eac8c798787a85578cc)), closes [#41](https://github.com/damienaulombard/pipewise/issues/41)


### Bug Fixes

* **eval:** list what either default-only branch depends on ([#44](https://github.com/damienaulombard/pipewise/issues/44)) ([4d57eea](https://github.com/damienaulombard/pipewise/commit/4d57eea8b7b5c17377d94f1c308c854b7f5ab10a)), closes [#42](https://github.com/damienaulombard/pipewise/issues/42)
