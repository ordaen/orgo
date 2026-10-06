package gql

import (
	"github.com/ordaen/orgo/logger"
	"github.com/ordaen/orgo/sessions"
)

// RegisterSchema registers the logs table the record writes are logged in and the sessions tables with pg.Schema,
// created by pg.Connect.
func RegisterSchema() {
	logger.RegisterSchema()
	sessions.RegisterSchema()
}
