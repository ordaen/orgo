# orgo

[![Go Reference](https://pkg.go.dev/badge/github.com/ordaen/orgo.svg)](https://pkg.go.dev/github.com/ordaen/orgo)

orgo is a Go library for building PostgreSQL-backed applications. It covers three things: the database
connection, the repositories that read and write your models, and the events published when records change.

```
go get github.com/ordaen/orgo
```

orgo requires Go 1.27 or later and PostgreSQL.

## Overview

### Database connection: `pg`

[`pg`](https://pkg.go.dev/github.com/ordaen/orgo/pg) connects the global `pg.DB` pool to PostgreSQL with
[pgx](https://github.com/jackc/pgx). It migrates the registered schema in one transaction and maps models,
plain structs, to table rows by their fields.

- The tables and functions are registered with `pg.Schema`, usually in `init` functions, and created by `pg.Connect`.
  The orgo packages with tables (`cron`, `files`, `logger`, `sessions`, `settings`, `statuser`, and `gql` for
  the tables it uses) register them with their `RegisterSchema` function, called before `pg.Connect`.
- `pg.Config.Schema` is created when it is missing and set as the `search_path` of every connection.
- `pg.Insert`, `pg.UpdateModel`, `pg.DeleteModel` and `pg.Query` write and query models.
- Models can implement hooks (`BeforeCreate`, `AfterUpdate`, …) that run inside the write transaction.
  `pg.WithTxHook` commits related writes, like a change record, together with the model write.
- Database errors can be turned into application errors by implementing `pg.DBErrorHandler`.
- Queries are logged with `log/slog`, or any `pg.Logger`. Query args are never logged.

### Repositories: `repo`, `model`, `cache`

A model embeds `model.Base` (an ID with the `created` and `updated` timestamps) or `model.CreateOnly`, and
names its table. A repository embeds `repo.Base`, which queries the database, or `repo.Cached`, which also keeps
the records in memory and loads them on connect. Each repository is registered once and lives in a package
variable:

```go
type User struct {
	model.Base[model.ID]
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (*User) TableName() string { return "users" }

var Users = repo.Register(&users{}, repo.WithEvents())

type users struct {
	repo.Base[*User]
}

func (r *users) FindByEmail(email string) *User {
	return r.FindWhere("email = ?", email)
}
```

Where conditions are raw SQL with `?` placeholders. A list for `IN` or `NOT IN` is passed with `pg.In`:
`Users.FindMany("id IN ?", pg.In(ids))`. `repo.WithContext` returns a copy of a repository that runs its queries
with a context.

### Events: `events`

[`events`](https://pkg.go.dev/github.com/ordaen/orgo/events) publishes named events to the subscribers of hubs.
Repositories created with `repo.WithEvents()` publish their committed changes to the `events.Creates`,
`events.Updates` and `events.Deletes` hubs, with the table name as the event name. `events.System` carries
application events.

```go
unsubscribe := events.Updates.Sub("users", func(e events.Event) {
	user := e.Doc().(*User)
	// ...
})
defer unsubscribe()
```

### Other packages

orgo also includes smaller packages that are built on the ones above or support them:

| Package     | Purpose                                                                 |
|-------------|-------------------------------------------------------------------------|
| `cron`      | Jobs run by cron specs, with their state and run logs stored in tables   |
| `statuser`  | Status machines for models, recording every status change               |
| `settings`  | Application settings structs stored as JSON in the `settings` table      |
| `sessions`  | Cached user sessions and blocked IP addresses and networks               |
| `logger`    | Audit logs of user actions with their changes                            |
| `gql`       | Helpers for GraphQL resolvers served with gin                           |
| `validator` | Collecting validation errors and returning them together                 |

There are also small utilities for configuration (`config`, `env`), encryption at rest (`crypt`), email
(`smtp`), websockets (`websocket`) and file storage (`files`). See the package documentation on
[pkg.go.dev](https://pkg.go.dev/github.com/ordaen/orgo) for details. Complete examples are coming.

## Contributing

Issues and pull requests are welcome. Please keep each change focused, and run `gofmt` and `go vet ./...`
before you submit.

### Testing requirements

Most packages are tested against a real PostgreSQL database, so a running server is required:

1. Create a database for the tests.
2. Create a `.env_test` file in the repository root (it is git-ignored):

   ```
   DATABASE_HOST=localhost:5432
   DATABASE_NAME=orgo
   DATABASE_USER=postgres
   DATABASE_PASSWORD=secret
   ```

3. Run all tests with the race detector:

   ```
   go test -race ./...
   ```

Each package tests in its own PostgreSQL schema (`orgo_cron`, `orgo_repo`, …), so packages can run in parallel
against the same database. The tests create and clear their tables. Use a dedicated database, not one with data
you need.

Pull requests must follow these rules:

- **Every change has tests**, and `go test -race ./...` passes.
- **Every exported identifier is documented.** Doc comments start with the name of the identifier and are
  checked with `go doc`.
- **Timed tests use [`testing/synctest`](https://pkg.go.dev/testing/synctest)**, not real sleeps. Tests that
  touch the database inside a bubble must use `pgtest.Synctest`, which opens all pool connections before the
  bubble starts, with a small `MaxConns` (5) in the package's test config. Opening or closing a pool
  connection inside a bubble is a fatal error.
- **Code must not panic on runtime input.** Return an error instead. A panic is acceptable only for
  definitions that are checked at application start, like `regexp.MustCompile`.

## License

[Apache License 2.0](LICENSE)
