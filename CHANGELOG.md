# Changelog

All notable changes of `multi-tour-worker-reprogramming-timers` (Reprogramming timers worker) are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [3.0.0] - 2026-11-14

Cut 3 release. Every entry below is a pull request merged into `qa` and re-applied to `release/3.0.0` with `git cherry-pick -x`; the heading of each group is the user story named in the pull request, and the entry is the title of the pull request without its type. The frozen head of `qa` is `a6cc2aeca08bef23eae808dbab87f27cba08e7a4`.

### Changed

#### HU-WRK-005 - Enforce the reprogramming deadlines

- The skeleton of the reprogramming timers worker ([PR 3](https://github.com/code-corhuila/multi-tour-worker-reprogramming-timers/pull/3))
- The job that expires the due rescheduling episodes ([PR 5](https://github.com/code-corhuila/multi-tour-worker-reprogramming-timers/pull/5))
- The HU-WRK-005 tests ([PR 8](https://github.com/code-corhuila/multi-tour-worker-reprogramming-timers/pull/8))
- The checks on pull requests to main ([PR 9](https://github.com/code-corhuila/multi-tour-worker-reprogramming-timers/pull/9))
- Run the test workflow only on pull requests to develop and qa ([PR 15](https://github.com/code-corhuila/multi-tour-worker-reprogramming-timers/pull/15))

### Fixed

#### HU-WRK-005 - Enforce the reprogramming deadlines

- Ask each due episode once per run ([PR 11](https://github.com/code-corhuila/multi-tour-worker-reprogramming-timers/pull/11))
- Make the expiry idempotency key stable per episode ([PR 13](https://github.com/code-corhuila/multi-tour-worker-reprogramming-timers/pull/13))

[Unreleased]: https://github.com/code-corhuila/multi-tour-worker-reprogramming-timers/compare/v3.0.0...HEAD
[3.0.0]: https://github.com/code-corhuila/multi-tour-worker-reprogramming-timers/releases/tag/v3.0.0
