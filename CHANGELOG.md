# Changelog

## [0.4.0](https://github.com/damienaulombard/pipewise/compare/v0.3.0...v0.4.0) (2026-11-23)


### Features

* **actions:** parse GitHub Actions workflows and reusable workflows ([adb1bc9](https://github.com/damienaulombard/pipewise/commit/adb1bc9e6641bee6f86e2f673a50048b3ec1b0f4)), closes [#33](https://github.com/damienaulombard/pipewise/issues/33)
* **actions:** parse GitHub Actions workflows and reusable workflows ([#72](https://github.com/damienaulombard/pipewise/issues/72)) ([adb1bc9](https://github.com/damienaulombard/pipewise/commit/adb1bc9e6641bee6f86e2f673a50048b3ec1b0f4))

## [0.3.0](https://github.com/damienaulombard/pipewise/compare/v0.2.0...v0.3.0) (2026-11-20)


### Features

* **rules:** add the no-cache rule ([#68](https://github.com/damienaulombard/pipewise/issues/68)) ([79bdc7a](https://github.com/damienaulombard/pipewise/commit/79bdc7a7e1d0ac3dce656586057bb861512bdee1)), closes [#32](https://github.com/damienaulombard/pipewise/issues/32)

## [0.2.0](https://github.com/damienaulombard/pipewise/compare/v0.1.0...v0.2.0) (2026-11-16)


### Public interface

* New JSON report: `pipewise explain --format json` (`schema`, `file`, `job`, `line`, and per event `event`, `verdict` and `decided_by`). Its field names are public interface, like those of `lint --format json`.
* Added to `decided_by` during the milestone: `line` (where the deciding clause is written, `0` for GitLab's default `only` or when no rule matched) and `from` (the top-level key holding it when it is not the job itself: a template reached through `extends`, an anchor or `!reference`); `except_line` and `except_from`, the same for the job's `except`.


### Features

* **cli:** add pipewise explain to show what decides each verdict ([#43](https://github.com/damienaulombard/pipewise/issues/43)) ([769f903](https://github.com/damienaulombard/pipewise/commit/769f903784a6d6cb0a030181de5878c2bf1ae1b1)), closes [#26](https://github.com/damienaulombard/pipewise/issues/26)
* **explain:** show where each deciding rule is written ([#45](https://github.com/damienaulombard/pipewise/issues/45)) ([6637f57](https://github.com/damienaulombard/pipewise/commit/6637f57163e6a74ced122eac8c798787a85578cc)), closes [#41](https://github.com/damienaulombard/pipewise/issues/41)


### Bug Fixes

* **eval:** list what either default-only branch depends on ([#44](https://github.com/damienaulombard/pipewise/issues/44)) ([4d57eea](https://github.com/damienaulombard/pipewise/commit/4d57eea8b7b5c17377d94f1c308c854b7f5ab10a)), closes [#42](https://github.com/damienaulombard/pipewise/issues/42)
