package actions

import (
	"github.com/ordaen/orgo/pg"
)

// RegisterSchema registers the action_logs table with pg.Schema, created by pg.Connect.
func RegisterSchema() {
	pg.Schema.Register("action_logs", SQLSchema)
}

// SQLSchema creates the action_logs table.
const SQLSchema = `CREATE TABLE IF NOT EXISTS action_logs (
			"id" Bigserial,
			"name" Text NOT NULL,
			"status" Text NOT NULL,
			"owner_id" Text,
			"owner_type" Text,
			"owner_name" Text,
			"progress" Int DEFAULT 0,
			"data" JSONB,
			"tasks" JSONB,
			"models" JSONB,
			"messages" JSONB,
			"issuer" JSONB,
			"created" Timestamptz DEFAULT now(),
			"updated" Timestamptz DEFAULT now(),
		PRIMARY KEY ( "id" ) );

CREATE INDEX IF NOT EXISTS action_logs_owner_id_type_idx ON action_logs(owner_id, owner_type);

CREATE INDEX IF NOT EXISTS action_logs_status_idx ON action_logs(status);

CREATE INDEX IF NOT EXISTS action_logs_updated_idx ON action_logs(updated DESC, id DESC);`
