package sessions

import (
	"github.com/ordaen/orgo/pg"
)

// RegisterSchema registers the sessions and blocked_ips tables with pg.Schema, created by pg.Connect.
func RegisterSchema() {
	pg.Schema.Register("sessions", SQLSchemaSessions)
	pg.Schema.Register("blocked_ips", SQLSchemaBlockedIPs)
}

// SQLSchemaSessions creates the sessions table
const SQLSchemaSessions = `CREATE TABLE IF NOT EXISTS sessions (
		"id" UUid NOT NULL,
		"token" UUid NOT NULL,
		"type" Text NOT NULL,
		"expires" Timestamptz NOT NULL,
		"super" Boolean DEFAULT FALSE NOT NULL,
		"user_id" Bigint,
		"user_type" Text,
		"user_name" Text,
		"user_ip" Text,
		"sso" Text,
		"created" Timestamptz DEFAULT now(),
		"updated" Timestamptz DEFAULT now(),
PRIMARY KEY ( "id", "token" ) );

CREATE INDEX IF NOT EXISTS sessions_token_idx ON sessions(token);

CREATE INDEX IF NOT EXISTS sessions_sso_idx ON sessions(sso);

CREATE INDEX IF NOT EXISTS sessions_user_id_idx ON sessions(user_id);

CREATE INDEX IF NOT EXISTS sessions_user_id_type_idx ON sessions(user_id,type);
`

// SQLSchemaBlockedIPs creates the blocked_ips table
const SQLSchemaBlockedIPs = `CREATE TABLE IF NOT EXISTS blocked_ips (
		"id" Bigserial,
		"ip" Cidr UNIQUE NOT NULL,
		"period" Bigint DEFAULT 0 NOT NULL,
		"reason" Text,
		"user_id" Bigint,
		"user_type" Text,
		"user_name" Text,
		"user_ip" Text,
		"created" Timestamptz DEFAULT now(),
		"updated" Timestamptz DEFAULT now(),
		PRIMARY KEY ( "id" ) );

CREATE INDEX IF NOT EXISTS blocked_ips_ip_idx ON blocked_ips USING GIST(ip inet_ops);
`
