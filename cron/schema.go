package cron

// SQLSchemaRecords creates the cron_records table
const SQLSchemaRecords = `CREATE TABLE IF NOT EXISTS cron_records (
			"id" Bigserial,
			"log_id" Bigint,
			"type" Text,
			"plugin" Text,
			"handler" Text NOT NULL,
			"name" Text NOT NULL,
			"spec" Text,
			"last_error" Text,
			"next_run" Timestamptz,
			"last_run" Timestamptz,
			"active" Boolean DEFAULT FALSE,
			"running" Boolean DEFAULT FALSE,
			"created" Timestamptz DEFAULT now(),
			"updated" Timestamptz DEFAULT now(),
PRIMARY KEY ( "id", "handler" ) );

ALTER TABLE cron_records ADD COLUMN IF NOT EXISTS "log_id" Bigint;`

// SQLSchemaLogs creates the cron_logs table
const SQLSchemaLogs = `CREATE TABLE IF NOT EXISTS cron_logs (
			"id" Bigserial,
			"handler" Text NOT NULL,
			"name" Text,
			"status" Text NOT NULL,
			"message" Text,
			"failures" Bigint DEFAULT 0,
			"protected" Boolean DEFAULT FALSE,
			"created" Timestamptz DEFAULT now(),
			"updated" Timestamptz DEFAULT now(),
PRIMARY KEY ( "id" ) );

CREATE INDEX IF NOT EXISTS cron_logs_handler_idx ON cron_logs(handler);

CREATE INDEX IF NOT EXISTS cron_logs_protected_idx ON cron_logs(protected);

CREATE INDEX IF NOT EXISTS cron_logs_sort_created_idx ON cron_logs USING btree (created Desc NULLS Last);`

// SQLSchemaLogMessages creates the cron_log_messages table
const SQLSchemaLogMessages = `CREATE TABLE IF NOT EXISTS cron_log_messages (
		"id" Bigserial,
		"log_id" Bigint NOT NULL,
		"message" Text NOT NULL,
		"error" Boolean DEFAULT FALSE,
		"created" Timestamptz DEFAULT now(),
		"updated" Timestamptz DEFAULT now(),
PRIMARY KEY ( "id" ) );

CREATE INDEX IF NOT EXISTS cron_log_messages_log_id_idx ON cron_log_messages(log_id);

CREATE INDEX IF NOT EXISTS cron_log_messages_error_idx ON cron_log_messages(error);

CREATE INDEX IF NOT EXISTS cron_log_messages_sort_created_idx ON cron_log_messages USING btree (created Desc NULLS Last);`
