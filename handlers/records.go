package handlers

import (
	"errors"
	"strings"

	"github.com/ordaen/orgo/logger"
	"github.com/ordaen/orgo/logs"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
)

// ScopedQuery returns a query of the records of r with the context and the scopes of the request.
func ScopedQuery[T model.Model](c *Ctx, r repo.Repository[T]) *pg.ModelQuery[T] {
	q := r.WithContext(c.Context()).Query()
	for _, s := range c.scopes {
		q = q.Where(s.where, s.args...)
	}
	return q
}

// GetRecords sets the response to the page of the records of r matching the list query of the request (see
// Ctx.Args) and the scopes, with the meta data of the page, or adds the error.
func GetRecords[T model.Model](c *Ctx, r repo.Repository[T]) {
	args, err := c.Args()
	if err != nil {
		c.AddError(err)
		return
	}
	q, err := ApplyQuery(ScopedQuery(c, r), args)
	if err != nil {
		c.AddError(err)
		return
	}
	total, err := q.Count()
	if err != nil {
		c.AddError(queryError(err, "count "+r.TableName()))
		return
	}
	recs, err := q.Select()
	if err != nil {
		c.AddError(queryError(err, "select from "+r.TableName()))
		return
	}
	c.SetFields(args.Fields)
	c.SetMeta(&MetaData{Total: total, Limit: args.Limit, Offset: args.Offset})
	c.SetData(recs)
}

// FetchRecord returns the record of r with the id in the scopes of the request, or adds ErrorRecordNotFound
// and returns false.
func FetchRecord[T model.Model](c *Ctx, r repo.Repository[T], id string) (T, bool) {
	var zero T
	ids, err := parseIDs(newModel[T](), []string{id})
	if err != nil || id == "" {
		c.AddError(ErrorRecordNotFound)
		return zero, false
	}
	rec, err := ScopedQuery(c, r).Where(`"id" IN ?`, ids).First()
	if err != nil {
		var pgErr *pg.PgError
		if errors.Is(err, pg.ErrRecordNotFound) || errors.As(err, &pgErr) && isDataException(pgErr) {
			// a missing record, or an id that is not a valid value of the column, like an invalid UUID
			c.AddError(ErrorRecordNotFound)
		} else {
			c.AddError(queryError(err, "find in "+r.TableName()))
		}
		return zero, false
	}
	return rec, true
}

// CreateRecord creates a record of r from the JSON body of the request, see DecodeRecord, and logs the creation by
// the user of the request, see logger.CreateRecord. It returns the record as stored, or adds the error and
// returns false.
func CreateRecord[T model.Model](c *Ctx, r repo.Repository[T]) (T, bool) {
	var zero T
	body, err := c.readBody()
	if err != nil {
		c.AddError(err)
		return zero, false
	}
	rec := r.New()
	if _, _, err := DecodeRecord(body, rec, true); err != nil {
		c.AddError(err)
		return zero, false
	}
	created, err := logger.CreateRecord(r.WithContext(c.Context()), rec, c.User())
	if err != nil {
		c.AddError(writeError(err, "create in "+r.TableName()))
		return zero, false
	}
	ClearCache(r.TableName())
	return created, true
}

// UpdateRecord updates the record of r from the JSON body of the request, see DecodeRecord, and logs the update by
// the user of the request with the changes, see logger.UpdateRecord. Only the changed columns are written,
// nothing when there are none. It returns the record as stored, or adds the error and returns false.
func UpdateRecord[T model.Model](c *Ctx, r repo.Repository[T], rec T) (T, bool) {
	var zero T
	body, err := c.readBody()
	if err != nil {
		c.AddError(err)
		return zero, false
	}
	ch, cols, err := DecodeRecord(body, rec, false)
	if err != nil {
		c.AddError(err)
		return zero, false
	}
	if len(cols) == 0 {
		return rec, true
	}
	updated, err := logger.UpdateRecord(r.WithContext(c.Context()), rec, c.User(), ch, cols...)
	if err != nil {
		c.AddError(writeError(err, "update in "+r.TableName()))
		return zero, false
	}
	ClearCache(r.TableName())
	return updated, true
}

// DeleteRecord deletes the record of r and logs the deletion by the user of the request, see logger.DeleteRecord,
// or adds the error and returns false.
func DeleteRecord[T model.Model](c *Ctx, r repo.Repository[T], rec T) bool {
	if err := logger.DeleteRecord(r.WithContext(c.Context()), rec, c.User()); err != nil {
		c.AddError(writeError(err, "delete in "+r.TableName()))
		return false
	}
	ClearCache(r.TableName())
	return true
}

// queryError returns the response error of a failed query: a data exception, like a value that is not a number
// for a number column, is a bad request, the other errors are logged and are an internal error without
// the details of the database.
func queryError(err error, op string) error {
	var pgErr *pg.PgError
	if errors.As(err, &pgErr) && isDataException(pgErr) {
		return ErrorBadRequest("invalid query value: " + pgErr.Message)
	}
	logs.Error(err, "handlers: "+op)
	return ErrorInternal("database error")
}

// writeError returns the response error of a failed write. The errors of the application, like the validation
// errors of the hooks and the errors of pg.DBErrorHandler, are returned as they are. A data exception is
// an invalid attribute, the other database errors are logged and are an internal error.
func writeError(err error, op string) error {
	var pgErr *pg.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	if isDataException(pgErr) || pgErr.Code == "23502" || pgErr.Code == "23514" {
		// a data exception, a not null or a check violation
		return ErrorInvalidAttribute(pgErr.ColumnName, pgErr.Message)
	}
	logs.Error(err, "handlers: "+op)
	return ErrorInternal("database error")
}

// isDataException reports whether the error is of the data exception class 22, like an invalid number.
func isDataException(err *pg.PgError) bool {
	return strings.HasPrefix(err.Code, "22")
}
