package settings

import (
	"github.com/ordaen/orgo/pg"
)

// RegisterSchema registers the settings table with pg.Schema, created by pg.Connect.
func RegisterSchema() {
	pg.Schema.Register("settings", SQLSchema)
}

// SQLSchema creates the settings table
const SQLSchema = `CREATE TABLE IF NOT EXISTS settings (
			"id" Bigserial,
			"type" Text NOT NULL,
			"data" JSONB,
			"created" Timestamptz DEFAULT now(),
			"updated" Timestamptz DEFAULT now(),
PRIMARY KEY ( "id" ) );

CREATE INDEX IF NOT EXISTS settings_type_idx ON settings(type);`
