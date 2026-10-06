package logger

import (
	"github.com/ordaen/orgo/pg"
)

// RegisterSchema registers the logs table with pg.Schema, created by pg.Connect.
func RegisterSchema() {
	pg.Schema.Register("logs", SQLSchema)
}

// SQLSchema creates the logs table
const SQLSchema = `CREATE TABLE IF NOT EXISTS logs (
			"id" Bigserial,
			"level" Text NOT NULL,
			"source" Text NOT NULL,
			"action" Text NOT NULL,
			"message" Text,
			"changes" JSONB,
			"data" JSONB,
			"owner_id" Text,
			"owner_type" Text,
			"user_id" Bigint,
			"user_type" Text,
			"user_name" Text,
			"user_ip" Text,
			"created" Timestamptz DEFAULT now(),
		PRIMARY KEY ( "id" ) );

CREATE INDEX IF NOT EXISTS logs_level_idx ON logs(level);

CREATE INDEX IF NOT EXISTS logs_source_idx ON logs(source);

CREATE INDEX IF NOT EXISTS logs_action_idx ON logs(action);

CREATE INDEX IF NOT EXISTS logs_owner_id_type_idx ON logs(owner_id,owner_type);

CREATE INDEX IF NOT EXISTS logs_user_id_type_idx ON logs(user_id,user_type);

CREATE INDEX IF NOT EXISTS logs_sort_created_idx ON logs USING btree (created Desc NULLS Last);`
