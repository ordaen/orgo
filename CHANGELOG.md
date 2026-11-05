# Changelog

All notable changes to orgo are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and orgo uses
[semantic versioning](https://semver.org/spec/v2.0.0.html). While orgo is on v0, a minor version can break
the API; breaking changes are listed under **Changed** or **Removed** and marked **Breaking**.

## [Unreleased]

### Added

- model: `WithSID`, a model with the `SID` of another system, and `OwnerID`, its SID or its ID.
- logger: `Log.WithOwner` sets the owner of a log to a record that is not a model, like an OpenStack resource.
- logger: `Log.SortBy` lists the logs with the last first in the gql querier.

### Changed

- logger: `Log.WithModel` stores the SID of a model with one as the owner ID, see `model.WithSID`.
- events: `Event.ID` returns the SID of a model with one, see `model.WithSID`, so `gql.GetIDFromEvent` and
  `websocket.ForwardEvents` send the SIDs of these records.
- gql: `Querier.Delete` logs the deleted record as the log data, like `logger.DeleteRecord`.

### Fixed

- gql: `Querier.Delete` uses the `Delete` method of a repository type embedding `repo.Base` or `repo.Cached`, it
  used the `Delete` of the embedded repository.
- logger: `CreateRecord` and `UpdateRecord` log the model given when the create or the update fails, they logged the
  returned record, which may be nil.

## [v0.5.0] - 2026-11-05

### Added

- model: `SID`, a string ID given by another system in any format, like the OpenStack resource IDs, which are not
  always UUIDs. It is encoded as a plain string in JSON and GraphQL.

### Fixed

- model: `UUID.UnmarshalJSON` leaves the UUID unchanged for a JSON `null`, it was set to `"null"`, and decodes the
  escaped characters. `UUID.MarshalJSON` escapes them. A value that is not a string, like a number, is an error.

## [v0.4.0] - 2026-11-05

### Added

- types: `User` has the `RoleID`, `Email` and `Avatar` fields of the user, which `UserFields` does not store.

### Removed

- types: the unused `scope:"admins"` struct tags of `UserFields`.

## [v0.3.1] - 2026-11-05

### Fixed

- pg: `BuildInsert`, and so `CreateModel` and `Insert`, keep a non-zero `created_at` or `updated_at` field set on the
  model, like a record imported with its original creation time, instead of always writing `NOW()`. A zero
  field is still set to `NOW()`.

## [v0.3.0] - 2026-11-05

### Added

- gql: `SetMaxLimit` sets the largest limit of `NewPagination` and `Querier.FindMany`, `MaxLimit` (100) by default.

## [v0.2.0] - 2026-11-05

### Added

- pg: `Config.NullZero` makes the model updates write the zero values, like `""`, `0` or a zero `time.Time`, as
  NULL, as go-pg does by default. A field tagged `db:",usezero"` is still written as it is. Inserts are not
  changed, they already leave the zero values to the column defaults.

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

[Unreleased]: https://github.com/ordaen/orgo/compare/v0.5.0...HEAD
[v0.5.0]: https://github.com/ordaen/orgo/compare/v0.4.0...v0.5.0
[v0.4.0]: https://github.com/ordaen/orgo/compare/v0.3.1...v0.4.0
[v0.3.1]: https://github.com/ordaen/orgo/compare/v0.3.0...v0.3.1
[v0.3.0]: https://github.com/ordaen/orgo/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/ordaen/orgo/compare/v0.1.1...v0.2.0
[v0.1.1]: https://github.com/ordaen/orgo/compare/v0.1.0...v0.1.1
[v0.1.0]: https://github.com/ordaen/orgo/releases/tag/v0.1.0
