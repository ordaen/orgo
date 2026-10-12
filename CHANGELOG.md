# Changelog

All notable changes to orgo are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and orgo uses
[semantic versioning](https://semver.org/spec/v2.0.0.html). While orgo is on v0, a minor version can break
the API; breaking changes are listed under **Changed** or **Removed** and marked **Breaking**.

## [Unreleased]

## [v0.1.1] - 2026-10-12

### Changed

- cron: parse the specs with `github.com/hashicorp/cronexpr`, the maintained fork, instead of the archived
  `github.com/gorhill/cronexpr`. In a timezone with daylight saving time, a daily job in the repeated hour of
  the fall-back night now runs twice, and a job in the skipped hour of the spring-forward day is skipped that day.

### Fixed

- cron: a day-of-week range ending in 7, like `5-7`, runs on those days, it never ran before.
- cron: a zero-padded month, like `01`, runs in that month, it ran on Dec 1 before.
- cron: a job in the skipped hour of the spring-forward day no longer runs back to back until the clocks jump.

## [v0.1.0] - 2026-10-07

The first tagged release.

[Unreleased]: https://github.com/ordaen/orgo/compare/v0.1.1...HEAD
[v0.1.1]: https://github.com/ordaen/orgo/compare/v0.1.0...v0.1.1
[v0.1.0]: https://github.com/ordaen/orgo/releases/tag/v0.1.0
