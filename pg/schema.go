package pg

import (
	"context"
	"embed"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
)

// Schema holds the queries creating the tables and the functions, run by ConnectWithContext.
// They are registered before Connect, usually in init functions, and removed after they run.
//
// The queries run in one transaction, so a failed migration changes nothing. Queries that cannot run in a
// transaction, like CREATE INDEX CONCURRENTLY, cannot be registered. A query may contain several statements.
var Schema = &schema{}

func migrateSchema(ctx context.Context, allowTableDrops bool) error {
	if err := Schema.migrate(ctx, allowTableDrops); err != nil {
		return err
	}
	Schema.clear()
	return nil
}

type schema struct {
	mu        sync.RWMutex
	tables    []schemaTable
	functions []string
}

// schemaTable is a table with its queries, in the registration order.
type schemaTable struct {
	name    string
	queries []string
}

// ListTables returns the tables with registered queries, in the registration order.
func (s *schema) ListTables() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]string, 0, len(s.tables))
	for _, t := range s.tables {
		res = append(res, t.name)
	}
	return res
}

// Register sets the queries of the table. The tables are migrated in the registration order, after the functions,
// so a table must be registered after the tables it references. Registering a table again replaces its queries
// and keeps its position. Queries with DROP TABLE, also in strings and DO blocks, fail the migration unless
// Config.AllowTableDrops is set.
func (s *schema) Register(table string, queries ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := slices.IndexFunc(s.tables, func(t schemaTable) bool { return t.name == table }); i >= 0 {
		s.tables[i].queries = queries
		return
	}
	s.tables = append(s.tables, schemaTable{name: table, queries: queries})
}

// RegisterFunction adds a query creating a function, run before the table queries in the registration order.
func (s *schema) RegisterFunction(content string) {
	s.mu.Lock()
	s.functions = append(s.functions, content)
	s.mu.Unlock()
}

// RegisterFiles registers the .sql files in the dir of fs in the file name order, like Register with the file name
// without .sql as the table and the file content as its query. Prefix the file names, like 01_users.sql,
// to register the referenced tables first. Other files are ignored.
func (s *schema) RegisterFiles(fs *embed.FS, dir string) error {
	files, err := fs.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		name := f.Name()
		if f.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		// embed.FS paths are always separated by slashes
		data, err := fs.ReadFile(path.Join(dir, name))
		if err != nil {
			return err
		}
		s.Register(strings.TrimSuffix(name, ".sql"), string(data))
	}
	return nil
}

func (s *schema) migrate(ctx context.Context, allowTableDrops bool) error {
	s.mu.RLock()
	functions := slices.Clone(s.functions)
	tables := slices.Clone(s.tables)
	s.mu.RUnlock()

	if len(functions) == 0 && len(tables) == 0 {
		return nil
	}
	if !allowTableDrops {
		for i, content := range functions {
			if containsDropTable(content) {
				return Errorf("migrate function %d: DROP TABLE is not allowed: [%s]", i+1, content)
			}
		}
		for _, t := range tables {
			for _, q := range t.queries {
				if containsDropTable(q) {
					return Errorf("migrate table %s: DROP TABLE is not allowed: [%s]", t.name, q)
				}
			}
		}
	}

	tx, err := DB.Begin(ctx)
	if err != nil {
		return Errorf("migrate: begin transaction: %w", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	for i, content := range functions {
		if err := execSchemaQuery(ctx, tx, content); err != nil {
			return Errorf("migrate function %d: %w", i+1, err)
		}
	}
	for _, t := range tables {
		for _, q := range t.queries {
			if err := execSchemaQuery(ctx, tx, q); err != nil {
				return Errorf("migrate table %s: %w", t.name, err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Errorf("migrate: commit transaction: %w", err)
	}
	return nil
}

// execSchemaQuery runs a registered query with the simple protocol, which allows several statements in a query.
func execSchemaQuery(ctx context.Context, tx Tx, query string) error {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, query, pgx.QueryExecModeSimpleProtocol); err != nil {
		return fmt.Errorf("%w: [%s]", err, strings.TrimSpace(query))
	}
	return nil
}

// clear removes the registered queries, so more can be registered for the next connect.
func (s *schema) clear() {
	s.mu.Lock()
	s.functions = nil
	s.tables = nil
	s.mu.Unlock()
}
