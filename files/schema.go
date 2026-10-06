package files

// SQLSchema creates the files table
const SQLSchema = `CREATE TABLE IF NOT EXISTS files (
		"id" Bigserial,
		"token" UUid NOT NULL,
		"name" Text NOT NULL,
		"type" Text,
		"mime" Text,
		"size" Bigint,
		"data" Bytea,
		"created" Timestamptz DEFAULT now(),
		"updated" Timestamptz DEFAULT now(),
		PRIMARY KEY ( "id" ) );

CREATE INDEX IF NOT EXISTS idx_files_token ON files ( "token" );`
