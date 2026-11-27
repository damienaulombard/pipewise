# Changelog

## [0.7.1](https://github.com/damienaulombard/pipewise/compare/v0.7.0...v0.7.1) (2026-11-27)


### Bug Fixes

* **config:** bound YAML alias expansion ([#101](https://github.com/damienaulombard/pipewise/issues/101)) ([c602076](https://github.com/damienaulombard/pipewise/commit/c6020765280207bcbc8ad436de92b715b8c1bf6c)), closes [#99](https://github.com/damienaulombard/pipewise/issues/99)

## [0.7.0](https://github.com/damienaulombard/pipewise/compare/v0.6.0...v0.7.0) (2026-11-27)


### Features

* **release:** push a Homebrew formula to the tap on each release ([#92](https://github.com/damienaulombard/pipewise/issues/92)) ([63fa520](https://github.com/damienaulombard/pipewise/commit/63fa52048f0797579e648a55a7b1336489c77c4a)), closes [#80](https://github.com/damienaulombard/pipewise/issues/80)

## [0.6.0](https://github.com/damienaulombard/pipewise/compare/v0.5.0...v0.6.0) (2026-11-25)


### Features

* **eval:** evaluate GitHub Actions if: expressions, tri-state ([#90](https://github.com/damienaulombard/pipewise/issues/90)) ([4a1e425](https://github.com/damienaulombard/pipewise/commit/4a1e425a44093fe5d41310ed59e71264ae189c97)), closes [#87](https://github.com/damienaulombard/pipewise/issues/87)
* **eval:** evaluate GitHub Actions on: trigger filters per event ([#91](https://github.com/damienaulombard/pipewise/issues/91)) ([087cfc8](https://github.com/damienaulombard/pipewise/commit/087cfc80cd71301c069aac1a345eafdd1274cad9)), closes [#88](https://github.com/damienaulombard/pipewise/issues/88)
* **rules:** add the stages-without-needs rule ([#77](https://github.com/damienaulombard/pipewise/issues/77)) ([2a4c65b](https://github.com/damienaulombard/pipewise/commit/2a4c65b9201f418904d26f60df778b331574e014)), closes [#30](https://github.com/damienaulombard/pipewise/issues/30)

## [0.5.0](https://github.com/damienaulombard/pipewise/compare/v0.4.0...v0.5.0) (2026-11-24)


### Features

* **fix:** print a unified diff for lint --fix ([#81](https://github.com/damienaulombard/pipewise/issues/81)) ([bf664fc](https://github.com/damienaulombard/pipewise/commit/bf664fc6a148c78f31b293ab292cd1a8d3452323)), closes [#27](https://github.com/damienaulombard/pipewise/issues/27)
* **rules:** add the e2e-everywhere rule ([#76](https://github.com/damienaulombard/pipewise/issues/76)) ([921403a](https://github.com/damienaulombard/pipewise/commit/921403a47ef755d90e222dfefe674e435f122086)), closes [#29](https://github.com/damienaulombard/pipewise/issues/29)
* **rules:** add the version-baked-at-build rule ([#78](https://github.com/damienaulombard/pipewise/issues/78)) ([dc6061d](https://github.com/damienaulombard/pipewise/commit/dc6061ddc15adba9ffa009b885fcc57278c8821b)), closes [#31](https://github.com/damienaulombard/pipewise/issues/31)

## [0.4.0](https://github.com/damienaulombard/pipewise/compare/v0.3.0...v0.4.0) (2026-11-23)


### Features

* **actions:** parse GitHub Actions workflows and reusable workflows ([#72](https://github.com/damienaulombard/pipewise/issues/72)) ([adb1bc9](https://github.com/damienaulombard/pipewise/commit/adb1bc9e6641bee6f86e2f673a50048b3ec1b0f4)), closes [#33](https://github.com/damienaulombard/pipewise/issues/33)

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
