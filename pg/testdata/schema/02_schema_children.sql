CREATE TABLE schema_children (
	"id" Bigserial PRIMARY KEY,
	"parent_id" Bigint NOT NULL REFERENCES schema_parents ("id")
);

CREATE FUNCTION schema_children_count() RETURNS Bigint AS $$
DECLARE
	n Bigint;
BEGIN
	SELECT COUNT(*) INTO n FROM schema_children;
	RETURN n;
END;
$$ LANGUAGE plpgsql;
