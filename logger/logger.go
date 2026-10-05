package logger

import (
	"github.com/ordaen/orgo/changes"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/types"
)

func NewLog(level LogLevel, source, action string) *Log {
	return &Log{Level: level, Source: source, Action: action}
}

func Info(action string) *Log {
	return NewLog(INFO, "system", action)
}

func Warn(action string) *Log {
	return NewLog(WARN, "system", action)
}

func Error(action string) *Log {
	return NewLog(ERROR, "system", action)
}

func Debug(action string) *Log {
	return NewLog(DEBUG, "system", action)
}

func UpdateRecord[T model.Model](m T, user *types.User, changes changes.Changes, fields ...string) error {
	mod, err := pg.UpdateModel(m, fields...)
	if err != nil {
		Error("update").WithUser(user).WithModel(mod).WithMessage(err.Error()).WithChanges(changes).Create()
		return err
	}
	Info("update").WithUser(user).WithModel(mod).WithChanges(changes).Create()
	return nil
}

func DeleteRecord[T model.Model](m T, user *types.User) error {
	mod, err := pg.DeleteModel(m)
	if err != nil {
		Error("delete").WithUser(user).WithModel(mod).WithMessage(err.Error()).Create()
		return err
	}
	Info("delete").WithUser(user).WithModel(mod).WithData(m).Create()
	return nil
}

func CreateRecord[T model.Model](m T, user *types.User) error {
	mod, err := pg.CreateModel(m)
	if err != nil {
		Error("create").WithUser(user).WithModel(mod).WithMessage(err.Error()).Create()
		return err
	}
	Info("create").WithUser(user).WithModel(mod).Create()
	return nil
}
