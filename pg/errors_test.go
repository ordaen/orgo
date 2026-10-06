package pg

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ordaen/orgo/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockedIP handles the database errors of its writes, it keeps the database error when handleDBErrors is not set.
type blockedIP struct {
	model.Base[model.ID]
	IP   string
	Code string
}

var (
	handleDBErrors bool
	handledOps     []string
)

func (m *blockedIP) TableName() string {
	return "blocked_ips"
}

func (m *blockedIP) HandleDBError(op string, err *PgError) error {
	handledOps = append(handledOps, op+":"+err.Code)
	if !handleDBErrors {
		return nil
	}
	switch err.ConstraintName {
	case "blocked_ips_ip_key":
		return fmt.Errorf("ip: %s already exists", m.IP)
	case "blocked_ips_code_key":
		return fmt.Errorf("code: %s already exists", m.Code)
	case "blocked_ip_logs_ip_id_fkey":
		return fmt.Errorf("ip: %s has logs", m.IP)
	}
	return nil
}

// plainBlockedIP has no DBErrorHandler.
type plainBlockedIP struct {
	model.Base[model.ID]
	IP string
}

func (m *plainBlockedIP) TableName() string {
	return "blocked_ips"
}

func setupDBErrorTest(t *testing.T) {
	t.Helper()
	_, err := DB.Exec(context.Background(), `
		DROP TABLE IF EXISTS blocked_ip_logs;
		DROP TABLE IF EXISTS blocked_ips;
		CREATE TABLE blocked_ips (
			"id" Bigserial PRIMARY KEY,
			"ip" Text UNIQUE,
			"code" Text UNIQUE DEFERRABLE INITIALLY DEFERRED,
			"created" Timestamptz DEFAULT now(),
			"updated" Timestamptz DEFAULT now());
		CREATE TABLE blocked_ip_logs (
			"id" Bigserial PRIMARY KEY,
			"ip_id" Bigint REFERENCES blocked_ips (id))`)
	require.NoError(t, err)
	handleDBErrors, handledOps = true, nil
	t.Cleanup(func() {
		handleDBErrors, handledOps = false, nil
		_, _ = DB.Exec(context.Background(), `DROP TABLE blocked_ip_logs; DROP TABLE blocked_ips`)
	})
}

func TestDBErrorHandler(t *testing.T) {
	setupDBErrorTest(t)

	first, err := CreateModel(&blockedIP{IP: "1.1.1.1", Code: "a"})
	require.NoError(t, err)

	// the handler error is returned unchanged
	_, err = CreateModel(&blockedIP{IP: "1.1.1.1", Code: "b"})
	assert.EqualError(t, err, "ip: 1.1.1.1 already exists")

	second, err := CreateModel(&blockedIP{IP: "2.2.2.2", Code: "b"})
	require.NoError(t, err)
	second.IP = "1.1.1.1"
	_, err = UpdateModel(second, "ip")
	assert.EqualError(t, err, "ip: 1.1.1.1 already exists")

	_, err = DB.Exec(context.Background(), `INSERT INTO blocked_ip_logs (ip_id) VALUES ($1)`, first.ID)
	require.NoError(t, err)
	_, err = DeleteModel(first)
	assert.EqualError(t, err, "ip: 1.1.1.1 has logs")

	// the deferred constraint fails on commit
	_, err = CreateModel(&blockedIP{IP: "3.3.3.3", Code: "a"})
	assert.EqualError(t, err, "code: a already exists")

	assert.Equal(t, []string{"create:23505", "update:23505", "delete:23503", "create:23505"}, handledOps)
	assert.Equal(t, 2, countRows(t, "blocked_ips"), "the failed writes are rolled back")
}

func TestDBErrorHandlerKeepsDatabaseError(t *testing.T) {
	setupDBErrorTest(t)

	_, err := CreateModel(&blockedIP{IP: "1.1.1.1", Code: "a"})
	require.NoError(t, err)

	// a nil result keeps the database error
	handleDBErrors = false
	_, err = CreateModel(&blockedIP{IP: "1.1.1.1", Code: "b"})
	require.ErrorContains(t, err, "[PG] rows iteration: ")
	var pgErr *PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, UniqueViolation, pgErr.Code)
	assert.Equal(t, []string{"create:23505"}, handledOps)

	// a model without a handler gets the database error
	_, err = CreateModel(&plainBlockedIP{IP: "1.1.1.1"})
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "blocked_ips_ip_key", pgErr.ConstraintName)

	// the errors without a PgError are not passed to the handler
	_, err = DB.Exec(context.Background(), `ALTER TABLE blocked_ips DROP CONSTRAINT blocked_ips_code_key, ALTER COLUMN code TYPE Point USING NULL, ALTER COLUMN code SET DEFAULT point(1, 2)`)
	require.NoError(t, err)
	_, err = CreateModel(&blockedIP{IP: "4.4.4.4"})
	require.ErrorContains(t, err, "[PG] scan: ")
	assert.Equal(t, []string{"create:23505"}, handledOps)
}

func TestKeyDetail(t *testing.T) {
	tests := []struct {
		detail          string
		columns, values string
		ok              bool
	}{
		{`Key (ip)=(1.1.1.1) already exists.`, "ip", "1.1.1.1", true},
		{`Key (name, email)=(john, john@example.com) already exists.`, "name, email", "john, john@example.com", true},
		{`Key (ip_id)=(5) is not present in table "blocked_ips".`, "ip_id", "5", true},
		{`Key (id)=(1) is still referenced from table "blocked_ip_logs".`, "id", "1", true},
		{`Key (name)=(a) b) already exists.`, "name", "a) b", true},
		{`Key (lower(name::text))=(john) already exists.`, "lower(name::text)", "john", true},
		{``, "", "", false},
		{`Failing row contains (1, null).`, "", "", false},
		{`Key (ip)`, "", "", false},
		{`Key (ip)=(1.1.1.1`, "", "", false},
	}
	for _, tt := range tests {
		columns, values, ok := KeyDetail(&PgError{Detail: tt.detail})
		assert.Equal(t, tt.ok, ok, tt.detail)
		assert.Equal(t, tt.columns, columns, tt.detail)
		assert.Equal(t, tt.values, values, tt.detail)
	}
}

func TestDBErrorHandlerIsNotWrapped(t *testing.T) {
	setupDBErrorTest(t)
	sentinel := errors.New("duplicate")
	_, err := CreateModel(&sentinelIP{IP: "1.1.1.1", err: sentinel})
	require.NoError(t, err)
	_, err = CreateModel(&sentinelIP{IP: "1.1.1.1", err: sentinel})
	assert.Same(t, sentinel, err)
}

// sentinelIP returns its err from the handler.
type sentinelIP struct {
	model.Base[model.ID]
	IP  string
	err error `db:"-"`
}

func (m *sentinelIP) TableName() string                    { return "blocked_ips" }
func (m *sentinelIP) HandleDBError(string, *PgError) error { return m.err }
