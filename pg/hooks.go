package pg

import (
	"context"
	"slices"
)

// BeforeCreateHook is implemented by models that run code before they are inserted.
// It is called inside the create transaction, before the insert query is built,
// so changes made to the model are inserted. Returning an error rolls the transaction back.
type BeforeCreateHook interface {
	BeforeCreate(ctx context.Context, tx Tx) error
}

// AfterCreateHook is implemented by models that run code after they are inserted.
// It is called inside the create transaction on the inserted record, as returned by the database,
// so generated values like the ID are set. Returning an error rolls the transaction back, including the insert.
type AfterCreateHook interface {
	AfterCreate(ctx context.Context, tx Tx) error
}

// BeforeUpdateHook is implemented by models that run code before they are updated.
// It is called inside the update transaction, before the update query is built,
// so changes made to the model are updated. When UpdateModel is called with fields,
// only these fields are written. Returning an error rolls the transaction back.
type BeforeUpdateHook interface {
	BeforeUpdate(ctx context.Context, tx Tx) error
}

// AfterUpdateHook is implemented by models that run code after they are updated.
// It is called inside the update transaction on the updated record, as returned by the database.
// Returning an error rolls the transaction back, including the update.
type AfterUpdateHook interface {
	AfterUpdate(ctx context.Context, tx Tx) error
}

// BeforeDeleteHook is implemented by models that run code before they are deleted.
// It is called inside the delete transaction on the given model. Returning an error rolls the transaction back.
type BeforeDeleteHook interface {
	BeforeDelete(ctx context.Context, tx Tx) error
}

// AfterDeleteHook is implemented by models that run code after they are deleted.
// It is called inside the delete transaction on the deleted record, as it was stored in the database.
// Returning an error rolls the transaction back, including the delete.
type AfterDeleteHook interface {
	AfterDelete(ctx context.Context, tx Tx) error
}

// DBErrorHandler is implemented by models that turn the database errors of their writes into application errors,
// like a unique violation into "ip: 1.1.1.1 already exists". It is called on the given model with the operation,
// "create", "update" or "delete", after the write or its commit failed. The transaction is rolled back.
// A non-nil result is returned unchanged in place of the database error, nil keeps the database error.
// The failed query of a handled error is not logged as failed, with Config.Debug it is logged at the debug level.
type DBErrorHandler interface {
	HandleDBError(op string, err *PgError) error
}

// TxHook runs in the transaction of a model write, see WithTxHook.
type TxHook func(ctx context.Context, tx Tx) error

// txHooksKey is the context key of the hooks added by WithTxHook.
type txHooksKey struct{}

// WithTxHook returns a copy of ctx running hook in the transaction of the model writes run with it, like
// UpdateModelWithContext and the repositories created by WithContext, after the After hook of the model and before
// the commit. An error of the hook rolls the write back and is returned unchanged. It commits the related writes
// with the model write, like a record of the change:
//
//	ctx = pg.WithTxHook(ctx, func(ctx context.Context, tx pg.Tx) error {
//		_, err := tx.Exec(ctx, "INSERT INTO changes ...")
//		return err
//	})
//	_, err := Orders.WithContext(ctx).Update(order, "status")
//
// The hooks of ctx run in the order they were added.
func WithTxHook(ctx context.Context, hook TxHook) context.Context {
	hooks, _ := ctx.Value(txHooksKey{}).([]TxHook)
	return context.WithValue(ctx, txHooksKey{}, append(slices.Clip(hooks), hook))
}

// runTxHooks runs the hooks added to ctx by WithTxHook.
func runTxHooks(ctx context.Context, tx Tx) error {
	hooks, _ := ctx.Value(txHooksKey{}).([]TxHook)
	for _, hook := range hooks {
		if err := hook(ctx, tx); err != nil {
			return err
		}
	}
	return nil
}
