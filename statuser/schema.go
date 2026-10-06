package statuser

// SQLSchema creates the status_records table
const SQLSchema = `CREATE TABLE IF NOT EXISTS status_records (
		"id" Bigserial,
		"status" Text NOT NULL,
		"reason" Text,
		"owner_id" Text NOT NULL,
		"owner_type" Text NOT NULL,
		"issuer" JSONB,
		"created" Timestamptz DEFAULT now(),
		PRIMARY KEY ( "id" ) );

DO $$ BEGIN
	-- owner_id was Bigint, Text holds the UUIDs too
	IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema()
		AND table_name = 'status_records' AND column_name = 'owner_id' AND data_type <> 'text') THEN
		ALTER TABLE status_records ALTER COLUMN owner_id TYPE Text;
	END IF;
END $$;

CREATE INDEX IF NOT EXISTS status_records_status_idx ON status_records(status);

CREATE INDEX IF NOT EXISTS status_records_owner_id_type_idx ON status_records(owner_id,owner_type);`
