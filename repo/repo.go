// Package repo provides repositories reading and writing the records of a table: Base queries the database,
// Cached also keeps the records in a cache. They can be embedded in application repository types with their own
// methods, initialized by Register and RegisterCached.
package repo

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ordaen/orgo/events"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
)

// Repository reads and writes the records of the table of T.
// Queries run with the context set by WithContext, or context.Background() when it is not set.
// The where conditions are raw SQL with ? placeholders for args, see pg.BindWhere, they may end with ORDER BY.
//
// The find and count methods do not return errors: when no record is found or the query fails, FindByID and FindWhere
// return a new model (a new struct when T is a pointer), FindMany returns an empty slice and Count and CountWhere
// return 0. The errors are logged with pg.DB.Logger(). Use Query to get the errors.
type Repository[T model.Model] interface {
	WithContext(ctx context.Context) Repository[T] // returns a copy of the repository using ctx
	New() T                                        // returns a new model, a new struct when T is a pointer
	FindByID(id any) T                             // returns the record with the given ID, or a new model
	FindWhere(where string, args ...any) T         // returns the first record matching the where condition, or a new model
	FindMany(where string, args ...any) []T        // returns the records matching the where condition, all records when it is empty
	Count() int                                    // returns the number of records, 0 on error
	CountWhere(where string, args ...any) int      // returns the number of records matching the where condition, of all records when it is empty, 0 on error
	Create(m T) (T, error)                         // creates the record and returns the created record
	Update(m T, fields ...string) (T, error)       // updates the record and returns the updated record
	Delete(m T) error                              // deletes the record
	TableName() string                             // returns the table name of the model
	Query() *pg.ModelQuery[T]                      // starts a query on the table with the repository context
}

var _ Repository[model.Model] = (*Base[model.Model])(nil)

// Option configures a repository created by New or NewCached.
type Option func(*options)

type options struct {
	events bool
}

// WithEvents makes the repository publish the records it creates, updates and deletes
// to events.Creates, events.Updates and events.Deletes. An event is published after the change is committed,
// failed changes are not published.
func WithEvents() Option {
	return func(o *options) {
		o.events = true
	}
}

// New creates a new instance of the repository for the table of m.
func New[T model.Model](m T, opts ...Option) *Base[T] {
	r := &Base[T]{}
	r.init(m, opts)
	return r
}

// initializer is a repository embedding Base or Cached, initialized by Register and RegisterCached.
type initializer interface {
	initRepository(opts []Option)
}

// Register initializes r, a repository type embedding Base, and returns it.
// The repository table is the table of the model type of Base:
//
//	var Users = repo.Register(&userRepository{}, repo.WithEvents())
//
//	type userRepository struct {
//		repo.Base[*User]
//	}
//
//	func (r *userRepository) FindByEmail(email string) *User {
//		return r.FindWhere("email = ?", email)
//	}
//
// Base must be embedded as a value, not as a pointer. A type embedding Cached is initialized like by RegisterCached,
// but it is not registered with pg.RegisterRepository, so its cache is not loaded on connect.
func Register[R initializer](r R, opts ...Option) R {
	r.initRepository(opts)
	return r
}

// WithContext returns a copy of r running its queries and model hooks with ctx. Unlike the WithContext method,
// it returns the type of r, so the methods of a repository type embedding Base or Cached can be used on the copy:
//
//	user := repo.WithContext(Users, r.Context()).FindByEmail(email)
//
// r is not changed. The copy is shallow, so it shares the cache of a Cached repository.
func WithContext[R interface{ setContext(context.Context) }](r R, ctx context.Context) R {
	v := reflect.ValueOf(r)
	c := reflect.New(v.Type().Elem())
	c.Elem().Set(v.Elem())
	res := c.Interface().(R)
	res.setContext(ctx)
	return res
}

// Base is a Repository using the global pg.DB. Create, Update and Delete call the model hooks,
// and publish events when the repository is created WithEvents.
// It can be embedded in a repository type initialized by Register.
type Base[T model.Model] struct {
	model       T
	tableName   string
	quotedTable string
	ctx         context.Context
	events      bool
}

// WithContext returns a copy of the repository running its queries and model hooks with ctx.
// The repository itself is not changed, so a single repository can be stored in a global variable
// and used with different contexts from different goroutines at the same time:
//
//	var Users = repo.New(&User{})
//	user := Users.WithContext(r.Context()).FindByID(id)
func (r *Base[T]) WithContext(ctx context.Context) Repository[T] {
	return WithContext(r, ctx)
}

func (r *Base[T]) initRepository(opts []Option) {
	r.init(newModel[T](), opts)
}

func (r *Base[T]) init(m T, opts []Option) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	table := m.TableName()
	*r = Base[T]{model: m, tableName: table, quotedTable: pg.QuoteTable(table), events: o.events}
}

func (r *Base[T]) setContext(ctx context.Context) {
	r.ctx = ctx
}

// FindByID returns the record with the id, or a new model when it is not found or the query fails.
func (r *Base[T]) FindByID(id any) T {
	return r.FindWhere(whereID, id)
}

// FindWhere returns the first record matching the where condition, or a new model when none matches
// or the query fails.
func (r *Base[T]) FindWhere(where string, args ...any) T {
	m, err := r.findOne(where, args...)
	if err != nil {
		r.logFailed("find", err)
		return newModel[T]()
	}
	return m
}

// FindMany returns the records matching the where condition, all records when it is empty.
// It returns an empty slice when no record matches or the query fails.
func (r *Base[T]) FindMany(where string, args ...any) []T {
	res, err := r.find(0, where, args...)
	if err != nil {
		r.logFailed("find", err)
	}
	if res == nil {
		return []T{}
	}
	return res
}

// Count returns the number of records in the table, 0 when the query fails.
// Cached repositories count them in the database too.
func (r *Base[T]) Count() int {
	n, err := pg.CountWithContext(r.context(), r.tableName)
	if err != nil {
		r.logFailed("count", err)
	}
	return n
}

// CountWhere returns the number of records matching the where condition, of all records when it is empty.
// It returns 0 when the query fails.
func (r *Base[T]) CountWhere(where string, args ...any) int {
	if where == "" {
		return r.Count()
	}
	n, err := pg.CountWhereWithContext(r.context(), r.tableName, where, args...)
	if err != nil {
		r.logFailed("count", err)
	}
	return n
}

// Create inserts m like pg.CreateModel and returns the inserted record.
func (r *Base[T]) Create(m T) (T, error) {
	return r.publish(events.Creates)(pg.CreateModelWithContext(r.context(), m))
}

// Update updates m like pg.UpdateModel and returns the updated record.
func (r *Base[T]) Update(m T, fields ...string) (T, error) {
	return r.publish(events.Updates)(pg.UpdateModelWithContext(r.context(), m, fields...))
}

// Delete deletes m like pg.DeleteModel.
func (r *Base[T]) Delete(m T) error {
	_, err := r.publish(events.Deletes)(pg.DeleteModelWithContext(r.context(), m))
	return err
}

// publish returns a function that publishes the committed record to the hub when events are enabled
// and the change did not fail. It passes the result through, so it wraps the model functions.
func (r *Base[T]) publish(hub *events.Hub) func(T, error) (T, error) {
	return func(m T, err error) (T, error) {
		if err == nil && r.events {
			hub.Pub(m)
		}
		return m, err
	}
}

// New returns a new model of the repository, a new struct when T is a pointer, to be filled and created.
func (r *Base[T]) New() T {
	return newModel[T]()
}

// TableName returns the repository table.
func (r *Base[T]) TableName() string {
	return r.tableName
}

// Query starts a query on the repository table, running with the repository context:
//
//	users, err := Users.WithContext(ctx).Query().Where("age > ?", 18).Order("name").Select()
func (r *Base[T]) Query() *pg.ModelQuery[T] {
	return pg.Query(r.model).WithContext(r.context())
}

// context returns the context set by WithContext, or context.Background() when it is not set.
func (r *Base[T]) context() context.Context {
	if r.ctx == nil {
		return context.Background()
	}
	return r.ctx
}

// logFailed logs the error of a find or count, which is not returned. Not found records are not errors,
// and database errors are not logged again, pg logs them with their query.
func (r *Base[T]) logFailed(op string, err error) {
	var pgErr *pgconn.PgError
	if errors.Is(err, pg.ErrRecordNotFound) || errors.As(err, &pgErr) {
		return
	}
	pg.DB.Logger().LogAttrs(r.context(), slog.LevelError, "orgo: "+op+" failed",
		slog.String("table", r.tableName), slog.Any("error", err))
}

// findOne returns the first record matching the where condition, or pg.ErrRecordNotFound.
func (r *Base[T]) findOne(where string, args ...any) (T, error) {
	res, err := r.find(1, where, args...)
	if err != nil {
		var zero T
		return zero, err
	}
	if len(res) == 0 {
		var zero T
		return zero, pg.ErrRecordNotFound
	}
	return res[0], nil
}

// find selects the records matching the where condition, at most limit records when limit > 0.
func (r *Base[T]) find(limit int, where string, args ...any) ([]T, error) {
	query := "SELECT * FROM " + r.quotedTable
	if where != "" {
		where, err := pg.BindWhere(where, len(args))
		if err != nil {
			return nil, err
		}
		query += " WHERE " + where
	}
	if limit > 0 {
		query += " LIMIT " + strconv.Itoa(limit)
	}

	rows, err := pg.DB.Query(r.context(), query, args...)
	if err != nil {
		return nil, pg.Errorf("select from %s: %w", r.tableName, err)
	}
	return pg.MapRowsLimited[T](rows, limit)
}

// whereID is the condition of FindByID.
var whereID = pg.QuoteIdent("id") + " = ?"

// newModel returns a new model of type T, a new struct when T is a pointer.
func newModel[T model.Model]() T {
	var m T
	if t := reflect.TypeFor[T](); t.Kind() == reflect.Pointer {
		m = reflect.New(t.Elem()).Interface().(T)
	}
	return m
}
