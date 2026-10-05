package pg

import "context"

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
type DBErrorHandler interface {
	HandleDBError(op string, err *PgError) error
}
