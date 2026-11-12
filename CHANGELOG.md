# Changelog

All notable changes to orgo are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and orgo uses
[semantic versioning](https://semver.org/spec/v2.0.0.html). While orgo is on v0, a minor version can break
the API; breaking changes are listed under **Changed** or **Removed** and marked **Breaking**.

## [Unreleased]

### Added

- handlers: a new package for REST controllers on gin, the port of the controllers of the core based apps. So far
  it has `ParseQuery` and `ApplyQuery`, which turn the URL query of a list request (`ids`, `limit`, `offset`, `sort`,
  `fields`, `filter[…]`, `query[…]`, `time[…]`) into a `pg.ModelQuery`, with the columns checked against the model,
  `Error`/`Errors`, the errors of the JSON responses with their HTTP status, and `Response`, the JSON document
  `{data, meta, errors}`. `EncodeData` encodes only the fields asked by the client and adds the computed
  fields of the models implementing `ModelWithExtraFields`. `DecodeRecord` sets the fields of a record from a
  request body, without the fields with the json option `readonly` (or `createonly` on update), and returns
  the changes and the changed columns for `logger.UpdateRecord`.
- pg: `FieldColumn`, the column of a model field.

## [v0.14.0] - 2026-11-12

### Added

- pg: `ModelQuery.CountUpTo`, which counts the matching records up to a max, so a count over a big table costs at
  most max rows.
- gql: `Querier.CountLimit`, which makes `FindManyPaginated` count up to a limit: past it, the total is the limit and
  the new `Pagination.TotalCapped` is true. Lists without it keep the exact count.

## [v0.13.1] - 2026-11-11

### Fixed

- logger, cron: the default sorts of `Log`, `CronLog` and `CronRecord` end with the id, so the pages of a paginated
  list do not overlap or skip records with the same time or name.

## [v0.13.0] - 2026-11-10

### Added

- websocket: `NewChanRequest`, whose `AuthorizeRequestFunc` also gets the request that opened the connection, e.g.
  to check the token against the client IP.

## [v0.12.0] - 2026-11-09

### Changed

- cron: a scheduled run claims its time by moving `next_run` to the following one, so instances scheduling the same
  jobs run each time once; the others skip it without logging.
- cron: a running job refreshes its record every minute. A lock without heartbeat for 5 minutes is left by a stopped
  process: a run takes it over and `Register` releases it. **Breaking**: `Register` no longer releases the lock of a
  live instance, and after a crash a job stays locked for up to 5 minutes.

## [v0.11.0] - 2026-11-06

### Added

- cron, statuser, sessions: `SortBy`, the default order of the gql querier when the client gives no sort:
  `CronLog` by `created DESC`, `CronLogMessage` and `StatusRecord` by `id DESC`, `CronRecord` by `name` and
  `BlockedIP` by `ip`.

## [v0.10.0] - 2026-11-05

### Added

- pg: `InsertTx` inserts a model without the model hooks in a transaction, like `Insert` on the connection.

## [v0.9.0] - 2026-11-05

### Added

- validator: `EmailFormat` and `PhoneFormat`, the formats of an email address and a phone number for `Format`,
  `RequiredFormat` and `ValidateFormat`.

## [v0.8.0] - 2026-11-05

### Added

- crypt: `NewDeterministicString` encrypts with a nonce derived from the plain string, the former `NewString`.
- crypt: `SetLegacyKey`, `LegacyPrefix` and `String.IsLegacy` decode the strings of the legacy format, AES-CFB
  prefixed with `:rsa:`. New strings are never written in this format.

### Changed

- crypt: **Breaking** `NewString` encrypts with a random nonce, so the same string encrypts to a different value
  each time. Use `NewDeterministicString` to find a value with an equality query.
- crypt: **Breaking** `String.MarshalJSON` writes the encrypted value instead of `"****"`, and `UnmarshalJSON` keeps
  an encrypted value as it is, so a `String` stored in a JSON column round-trips. GraphQL still masks it, and
  `UnmarshalGQL` always encrypts the value it receives.

## [v0.7.0] - 2026-11-05

### Added

- model: `ParseID` returns the ID in a decimal string, or 0 when the string is not an integer.

## [v0.6.0] - 2026-11-05

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

[Unreleased]: https://github.com/ordaen/orgo/compare/v0.14.0...HEAD
[v0.14.0]: https://github.com/ordaen/orgo/compare/v0.13.1...v0.14.0
[v0.13.1]: https://github.com/ordaen/orgo/compare/v0.13.0...v0.13.1
[v0.13.0]: https://github.com/ordaen/orgo/compare/v0.12.0...v0.13.0
[v0.12.0]: https://github.com/ordaen/orgo/compare/v0.11.0...v0.12.0
[v0.11.0]: https://github.com/ordaen/orgo/compare/v0.10.0...v0.11.0
[v0.10.0]: https://github.com/ordaen/orgo/compare/v0.9.0...v0.10.0
[v0.9.0]: https://github.com/ordaen/orgo/compare/v0.8.0...v0.9.0
[v0.8.0]: https://github.com/ordaen/orgo/compare/v0.7.0...v0.8.0
[v0.7.0]: https://github.com/ordaen/orgo/compare/v0.6.0...v0.7.0
[v0.6.0]: https://github.com/ordaen/orgo/compare/v0.5.0...v0.6.0
[v0.5.0]: https://github.com/ordaen/orgo/compare/v0.4.0...v0.5.0
[v0.4.0]: https://github.com/ordaen/orgo/compare/v0.3.1...v0.4.0
[v0.3.1]: https://github.com/ordaen/orgo/compare/v0.3.0...v0.3.1
[v0.3.0]: https://github.com/ordaen/orgo/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/ordaen/orgo/compare/v0.1.1...v0.2.0
[v0.1.1]: https://github.com/ordaen/orgo/compare/v0.1.0...v0.1.1
[v0.1.0]: https://github.com/ordaen/orgo/releases/tag/v0.1.0
