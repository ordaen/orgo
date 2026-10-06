// Package logger stores the audit logs of the application in the logs table: the actions by the users, with
// their changes, data and the record they are about.
//
//	logger.Info("update").WithUser(user).WithModel(order).WithChanges(changes).Create()
package logger

import (
	"github.com/ordaen/orgo/changes"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/repo"
	"github.com/ordaen/orgo/types"
)

// NewLog returns a new log of the action from the source with the level, it is stored by Create.
func NewLog(level LogLevel, source, action string) *Log {
	return &Log{Level: level, Source: source, Action: action}
}

// Info returns a new info log of the action from the "system" source.
func Info(action string) *Log {
	return NewLog(INFO, "system", action)
}

// Warn returns a new warning log of the action from the "system" source.
func Warn(action string) *Log {
	return NewLog(WARN, "system", action)
}

// Error returns a new error log of the action from the "system" source.
func Error(action string) *Log {
	return NewLog(ERROR, "system", action)
}

// Debug returns a new debug log of the action from the "system" source.
func Debug(action string) *Log {
	return NewLog(DEBUG, "system", action)
}

// CreateRecord creates the record with the repository and logs the creation by the user.
// It returns the created record, the error is logged too.
func CreateRecord[T model.Model](r repo.Repository[T], m T, user *types.User) (T, error) {
	mod, err := r.Create(m)
	if err != nil {
		Error("create").WithUser(user).WithModel(mod).WithMessage(err.Error()).Create()
		return mod, err
	}
	Info("create").WithUser(user).WithModel(mod).Create()
	return mod, nil
}

// UpdateRecord updates the record with the repository and logs the changes by the user.
// When fields are given, only these columns are updated. It returns the updated record, the error is logged too.
func UpdateRecord[T model.Model](r repo.Repository[T], m T, user *types.User, changes changes.Changes, fields ...string) (T, error) {
	mod, err := r.Update(m, fields...)
	if err != nil {
		Error("update").WithUser(user).WithModel(mod).WithMessage(err.Error()).WithChanges(changes).Create()
		return mod, err
	}
	Info("update").WithUser(user).WithModel(mod).WithChanges(changes).Create()
	return mod, nil
}

// DeleteRecord deletes the record with the repository and logs the deletion by the user, with the deleted record
// as the log data. The error is logged too.
func DeleteRecord[T model.Model](r repo.Repository[T], m T, user *types.User) error {
	if err := r.Delete(m); err != nil {
		Error("delete").WithUser(user).WithModel(m).WithMessage(err.Error()).Create()
		return err
	}
	Info("delete").WithUser(user).WithModel(m).WithData(m).Create()
	return nil
}
