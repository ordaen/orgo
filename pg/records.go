package pg

import (
	"context"
	"errors"
	"fmt"
)

// CreateModel inserts the model and returns the inserted record as it is stored in the database.
// The fields with zero values are not inserted, they get the column defaults, see BuildInsert.
func CreateModel[T Model](model T) (T, error) {
	return CreateModelWithContext(context.Background(), model)
}

// Insert inserts the model like CreateModel, but in a single query without a transaction and without the model hooks.
// It returns the inserted record as it is stored in the database. The DBErrorHandler of the model is still called
// with the "create" operation. When the inserted row cannot be mapped to the model the error is returned,
// but the row stays inserted.
func Insert[T Model](model T) (T, error) {
	return InsertWithContext(context.Background(), model)
}

// InsertWithContext is like Insert, but runs the query with ctx.
func InsertWithContext[T Model](ctx context.Context, model T) (T, error) {
	query, err := BuildInsert(model)
	if err != nil {
		return model, err
	}
	rows, err := DB.Query(ctx, query.SQL, query.Args...)
	if err != nil {
		return model, handleDBError(model, "create", Errorf("insert into %s: %w", model.TableName(), err))
	}
	res, err := MapRowsLimited[T](rows, 1)
	if err != nil {
		return model, handleDBError(model, "create", err)
	}
	if len(res) == 0 {
		return model, ErrNoRowsAffected
	}
	return res[0], nil
}

// UpdateModel updates the model by its ID and returns the updated record as it is stored in the database.
// When fields are given, only these columns are updated. The updated_at column is always set to NOW().
// It returns ErrNoRowsAffected when there is no record with the model ID.
func UpdateModel[T Model](model T, fields ...string) (T, error) {
	return UpdateModelWithContext(context.Background(), model, fields...)
}

// DeleteModel deletes the model by its ID and returns the deleted record as it was stored in the database.
// It returns ErrNoRowsAffected when there is no record with the model ID.
func DeleteModel[T Model](model T) (T, error) {
	return DeleteModelWithContext(context.Background(), model)
}

// CreateModelWithContext is like CreateModel, but runs the queries and the hooks with ctx.
func CreateModelWithContext[T Model](ctx context.Context, model T) (T, error) {
	return writeModel(ctx, model, writeOp[T]{
		name: "create",
		verb: "insert into",
		check: func(m T) error {
			_, _, err := modelFor(m)
			return err
		},
		build: func(m T) (*PlainQuery, error) {
			return BuildInsert(m)
		},
		before: func(ctx context.Context, tx Tx, m T) error {
			if hook, ok := any(m).(BeforeCreateHook); ok {
				return hook.BeforeCreate(ctx, tx)
			}
			return nil
		},
		after: func(ctx context.Context, tx Tx, m T) error {
			if hook, ok := any(m).(AfterCreateHook); ok {
				return hook.AfterCreate(ctx, tx)
			}
			return nil
		},
	})
}

// UpdateModelWithContext is like UpdateModel, but runs the queries and the hooks with ctx.
func UpdateModelWithContext[T Model](ctx context.Context, model T, fields ...string) (T, error) {
	return writeModel(ctx, model, writeOp[T]{
		name: "update",
		verb: "update",
		check: func(m T) error {
			info, _, err := modelFor(m)
			if err != nil {
				return err
			}
			return checkUpdate(info, m, fields)
		},
		build: func(m T) (*PlainQuery, error) {
			return BuildUpdate(m, fields...)
		},
		before: func(ctx context.Context, tx Tx, m T) error {
			if hook, ok := any(m).(BeforeUpdateHook); ok {
				return hook.BeforeUpdate(ctx, tx)
			}
			return nil
		},
		after: func(ctx context.Context, tx Tx, m T) error {
			if hook, ok := any(m).(AfterUpdateHook); ok {
				return hook.AfterUpdate(ctx, tx)
			}
			return nil
		},
	})
}

// DeleteModelWithContext is like DeleteModel, but runs the queries and the hooks with ctx.
func DeleteModelWithContext[T Model](ctx context.Context, model T) (T, error) {
	return writeModel(ctx, model, writeOp[T]{
		name: "delete",
		verb: "delete from",
		check: func(m T) error {
			info, _, err := modelFor(m)
			if err != nil {
				return err
			}
			return checkID(info, m)
		},
		build: func(m T) (*PlainQuery, error) {
			return BuildDelete(m)
		},
		before: func(ctx context.Context, tx Tx, m T) error {
			if hook, ok := any(m).(BeforeDeleteHook); ok {
				return hook.BeforeDelete(ctx, tx)
			}
			return nil
		},
		after: func(ctx context.Context, tx Tx, m T) error {
			if hook, ok := any(m).(AfterDeleteHook); ok {
				return hook.AfterDelete(ctx, tx)
			}
			return nil
		},
	})
}

// writeOp describes a write query of a model with its hooks.
type writeOp[T Model] struct {
	name   string                                      // operation name passed to DBErrorHandler: "create", "update", "delete"
	verb   string                                      // SQL verb in query errors: "insert into", "update", "delete from"
	check  func(T) error                               // validates the model before the transaction starts and hooks are called
	build  func(T) (*PlainQuery, error)                // builds the query after the before hook, so its changes are written
	before func(ctx context.Context, tx Tx, m T) error // called on the given model
	after  func(ctx context.Context, tx Tx, m T) error // called on the written record returned by the database
}

// writeModel runs the write query and the hooks in one transaction, an error in any of them rolls everything back.
// It also rolls the write back if the returned row cannot be mapped to the model.
func writeModel[T Model](ctx context.Context, model T, op writeOp[T]) (T, error) {
	if err := op.check(model); err != nil {
		return model, err
	}

	tx, err := DB.Begin(ctx)
	if err != nil {
		return model, Errorf("begin transaction: %w", err)
	}
	// the rollback must run also when ctx is canceled, it is what ends the transaction then
	defer tx.Rollback(context.WithoutCancel(ctx))

	// the hook errors are returned unchanged, they are the errors of the application
	if err := op.before(ctx, tx, model); err != nil {
		return model, err
	}

	query, err := op.build(model)
	if err != nil {
		return model, err
	}
	rows, err := tx.Query(ctx, query.SQL, query.Args...)
	if err != nil {
		return model, handleDBError(model, op.name, Errorf("%s %s: %w", op.verb, model.TableName(), err))
	}
	// the constraint violations of the write are returned by the rows
	res, err := MapRowsLimited[T](rows, 1)
	if err != nil {
		return model, handleDBError(model, op.name, err)
	}
	if len(res) == 0 {
		return model, ErrNoRowsAffected
	}
	written := res[0]

	if err := op.after(ctx, tx, written); err != nil {
		return model, err
	}
	if err := runTxHooks(ctx, tx); err != nil {
		return model, err
	}

	// the deferred constraints are checked on commit
	if err := tx.Commit(ctx); err != nil {
		return model, handleDBError(model, op.name, Errorf("commit transaction: %w", err))
	}
	return written, nil
}

// handleDBError returns the error of the DBErrorHandler of model for the PgError in err,
// or err when there is no PgError, no handler or the handler returns nil.
func handleDBError[T Model](model T, op string, err error) error {
	handler, ok := any(model).(DBErrorHandler)
	if !ok {
		return err
	}
	var pgErr *PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	if handled := handler.HandleDBError(op, pgErr); handled != nil {
		return handled
	}
	return err
}

// Count returns the number of rows in the table.
func Count(table string) (int, error) {
	return CountWithContext(context.Background(), table)
}

// CountWhere returns the number of rows in the table matching the condition.
// The condition is raw SQL with ? placeholders for args, see BindWhere. Reserved word columns in it must be quoted.
func CountWhere(table string, where string, args ...any) (int, error) {
	return CountWhereWithContext(context.Background(), table, where, args...)
}

// CountWithContext is like Count, but runs the query with ctx.
func CountWithContext(ctx context.Context, table string) (int, error) {
	return count(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", QuoteTable(table)), table)
}

// CountWhereWithContext is like CountWhere, but runs the query with ctx.
func CountWhereWithContext(ctx context.Context, table string, where string, args ...any) (int, error) {
	where, args, err := BindWhere(where, args...)
	if err != nil {
		return 0, err
	}
	return count(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", QuoteTable(table), where), table, args...)
}

func count(ctx context.Context, query string, table string, args ...any) (int, error) {
	var n int
	if err := DB.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		return 0, Errorf("count %s: %w", table, err)
	}
	return n, nil
}

// Exec runs sql, which does not return rows, on DB and returns the number of rows it affected.
func Exec(sql string, args ...any) (int64, error) {
	return ExecWithContext(context.Background(), sql, args...)
}

// ExecWithContext is like Exec, but runs sql with ctx.
func ExecWithContext(ctx context.Context, sql string, args ...any) (int64, error) {
	tag, err := DB.Exec(ctx, sql, args...)
	if err != nil {
		return 0, Errorf("exec: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ClearTables deletes all rows from the tables.
func ClearTables(tables ...string) error {
	for _, table := range tables {
		query := fmt.Sprintf("DELETE FROM %s", QuoteTable(table))
		if _, err := DB.Exec(context.Background(), query); err != nil {
			return Errorf("clear %s: %w", table, err)
		}
	}
	return nil
}
