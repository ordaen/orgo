package pg

import (
	"context"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// ModelQuery builds and runs a SELECT or DELETE query on the table of T:
//
//	users, err := pg.Query(&User{}).Where("age > ?", 18).Order("name").Limit(10).Select()
//
// Every method returns a new query, so a query can be used as a base for other queries
// and shared between goroutines.
type ModelQuery[T Model] struct {
	tableName   string
	quotedTable string
	ctx         context.Context
	wheres      []string
	args        []any
	orders      []string
	limit       int
	offset      int
	err         error
}

// Query starts a query on the table of m.
func Query[T Model](m T) *ModelQuery[T] {
	info := globalCache.getModelInfo(reflect.TypeFor[T]())
	if info.quotedTable != "" {
		return &ModelQuery[T]{tableName: info.tableName, quotedTable: info.quotedTable}
	}
	return &ModelQuery[T]{tableName: m.TableName(), quotedTable: QuoteTable(m.TableName())}
}

// WithContext returns a query running with ctx. Without it context.Background() is used.
func (q *ModelQuery[T]) WithContext(ctx context.Context) *ModelQuery[T] {
	c := q.clone()
	c.ctx = ctx
	return c
}

// Where adds a condition. It is raw SQL with ? placeholders for args, see BindWhere, they are numbered when the
// query is built. Conditions of multiple Where calls are joined with AND.
// An empty condition is ignored.
func (q *ModelQuery[T]) Where(where string, args ...any) *ModelQuery[T] {
	c := q.clone()
	if where == "" || c.err != nil {
		return c
	}
	sql, args, err := bindPlaceholders(where, len(c.args), args)
	if err != nil {
		c.err = err
		return c
	}
	c.wheres = append(c.wheres, "("+sql+")")
	c.args = append(c.args, args...)
	return c
}

// Order adds an ORDER BY expression, like "name" or "created DESC". Expressions of multiple Order calls
// are joined in the call order. An empty expression is ignored.
func (q *ModelQuery[T]) Order(order string) *ModelQuery[T] {
	c := q.clone()
	if order != "" {
		c.orders = append(c.orders, order)
	}
	return c
}

// Limit sets the maximum number of records. 0 removes the limit.
func (q *ModelQuery[T]) Limit(limit int) *ModelQuery[T] {
	c := q.clone()
	c.limit = max(limit, 0)
	return c
}

// Offset sets the number of records to skip. 0 removes the offset.
func (q *ModelQuery[T]) Offset(offset int) *ModelQuery[T] {
	c := q.clone()
	c.offset = max(offset, 0)
	return c
}

// Select returns the matching records, or an empty slice when there are none.
func (q *ModelQuery[T]) Select() ([]T, error) {
	if q.err != nil {
		return nil, q.err
	}
	sql, args := q.build("*", true)
	rows, err := DB.Query(orBackground(q.ctx), sql, args...)
	if err != nil {
		return nil, Errorf("select from %s: %w", q.tableName, err)
	}
	res, err := MapRows[T](rows)
	if err != nil {
		return nil, err
	}
	if res == nil {
		res = []T{}
	}
	return res, nil
}

// First returns the first matching record, or ErrRecordNotFound.
func (q *ModelQuery[T]) First() (T, error) {
	res, err := q.Limit(1).Select()
	if err != nil {
		var zero T
		return zero, err
	}
	if len(res) == 0 {
		var zero T
		return zero, ErrRecordNotFound
	}
	return res[0], nil
}

// Count returns the number of matching records. Order, Limit and Offset are ignored.
func (q *ModelQuery[T]) Count() (int, error) {
	if q.err != nil {
		return 0, q.err
	}
	sql, args := q.build("COUNT(*)", false)
	var n int
	if err := DB.QueryRow(orBackground(q.ctx), sql, args...).Scan(&n); err != nil {
		return 0, Errorf("count %s: %w", q.tableName, err)
	}
	return n, nil
}

// Delete deletes the matching records without the model hooks and returns the number of deleted records.
// Order is ignored. It returns an error without a condition, use ClearTables to delete all records,
// and with Limit or Offset, which DELETE does not support.
func (q *ModelQuery[T]) Delete() (int64, error) {
	if q.err != nil {
		return 0, q.err
	}
	if len(q.wheres) == 0 {
		return 0, Errorf("delete from %s: no condition", q.tableName)
	}
	if q.limit > 0 || q.offset > 0 {
		return 0, Errorf("delete from %s: limit and offset are not supported", q.tableName)
	}
	sql, args := q.buildDelete()
	tag, err := DB.Exec(orBackground(q.ctx), sql, args...)
	if err != nil {
		return 0, Errorf("delete from %s: %w", q.tableName, err)
	}
	return tag.RowsAffected(), nil
}

// buildDelete returns the DELETE SQL and its args.
func (q *ModelQuery[T]) buildDelete() (string, []any) {
	var b strings.Builder
	b.WriteString("DELETE FROM ")
	b.WriteString(q.quotedTable)
	q.writeWhere(&b)
	return b.String(), q.args
}

// build returns the SQL and its args. Order, limit and offset are added when paging is true.
func (q *ModelQuery[T]) build(columns string, paging bool) (string, []any) {
	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(columns)
	b.WriteString(" FROM ")
	b.WriteString(q.quotedTable)
	q.writeWhere(&b)
	if paging {
		if len(q.orders) > 0 {
			b.WriteString(" ORDER BY ")
			writeJoined(&b, q.orders, ", ")
		}
		if q.limit > 0 {
			b.WriteString(" LIMIT ")
			b.WriteString(strconv.Itoa(q.limit))
		}
		if q.offset > 0 {
			b.WriteString(" OFFSET ")
			b.WriteString(strconv.Itoa(q.offset))
		}
	}
	return b.String(), q.args
}

// writeWhere writes the WHERE clause, nothing when there are no conditions.
func (q *ModelQuery[T]) writeWhere(b *strings.Builder) {
	if len(q.wheres) > 0 {
		b.WriteString(" WHERE ")
		writeJoined(b, q.wheres, " AND ")
	}
}

// writeJoined writes the parts separated by sep, like strings.Join without the intermediate string.
func writeJoined(b *strings.Builder, parts []string, sep string) {
	for i, p := range parts {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(p)
	}
}

func (q *ModelQuery[T]) clone() *ModelQuery[T] {
	c := *q
	c.wheres = slices.Clone(q.wheres)
	c.args = slices.Clone(q.args)
	c.orders = slices.Clone(q.orders)
	return &c
}

// In is the list of values of an IN or NOT IN condition, bound as one array parameter:
//
//	Query(&User{}).Where("id IN ?", pg.In(ids))
//
// "x IN ?" is bound as "x = ANY($1)" and "x NOT IN ?" as "x <> ALL($1)", so the query is the same for any
// number of values, an empty list matches no record with IN and every record with NOT IN. A nil list is empty.
func In[S ~[]E, E any](values S) InList {
	if values == nil {
		values = S{}
	}
	return InList{values: values}
}

// InList is the list of values of an IN condition, see In.
type InList struct {
	values any
}

// BindWhere returns the where condition with its ? placeholders replaced by the $1, $2... placeholders of PostgreSQL,
// and the args to run it with. It returns an error when the number of placeholders differs from the number of args.
// A doubled ?? is a literal ?, like the jsonb operators: "tags ?? ?" is "tags ? $1". Placeholders in strings,
// quoted identifiers, dollar quoted strings and comments are not replaced. The In args are bound with their
// IN or NOT IN, see In.
func BindWhere(where string, args ...any) (string, []any, error) {
	return bindPlaceholders(where, 0, args)
}

// bindPlaceholders replaces the ? placeholders of the SQL with $offset+1, $offset+2... and returns the args to
// run it with, the In args replaced by their values. It fails when the number of placeholders differs from the
// number of args, or an In arg does not follow IN or NOT IN. A doubled ?? is a literal ?. Strings, quoted
// identifiers, dollar quoted strings and comments are copied unchanged. args is not modified.
func bindPlaceholders(sql string, offset int, args []any) (string, []any, error) {
	var b strings.Builder
	b.Grow(len(sql) + 8)
	n := 0
	cloned := false
	for i := 0; i < len(sql); {
		c := sql[i]
		end := i + 1
		switch {
		case c == '?' && end < len(sql) && sql[end] == '?':
			b.WriteByte('?')
			i += 2
			continue
		case c == '?':
			n++
			placeholder := "$" + strconv.Itoa(offset+n)
			if n <= len(args) {
				if list, ok := args[n-1].(InList); ok {
					prefix, op, ok := cutInKeyword(b.String())
					if !ok {
						return "", nil, Errorf("where %q: pg.In arg %d does not follow IN or NOT IN", sql, n)
					}
					if !cloned {
						args, cloned = slices.Clone(args), true
					}
					args[n-1] = list.values
					b.Reset()
					b.WriteString(prefix)
					placeholder = op + "(" + placeholder + ")"
				}
			}
			b.WriteString(placeholder)
			i++
			continue
		case strings.HasPrefix(sql[i:], "--"):
			end = len(sql)
			if j := strings.IndexByte(sql[i:], '\n'); j >= 0 {
				end = i + j + 1
			}
		case strings.HasPrefix(sql[i:], "/*"):
			end = skipBlockComment(sql, i)
		case c == '\'':
			_, end = scanQuoted(sql, i, '\'', false)
		case (c == 'e' || c == 'E') && end < len(sql) && sql[end] == '\'' && (i == 0 || !isIdentChar(sql[i-1])):
			_, end = scanQuoted(sql, end, '\'', true)
		case c == '"':
			_, end = scanQuoted(sql, i, '"', false)
		case c == '$':
			if tag, ok := dollarTag(sql, i); ok {
				start := i + len(tag)
				end = len(sql)
				if j := strings.Index(sql[start:], tag); j >= 0 {
					end = start + j + len(tag)
				}
			}
		}
		b.WriteString(sql[i:end])
		i = end
	}
	if n != len(args) {
		return "", nil, Errorf("where %q: %d placeholders, %d args", sql, n, len(args))
	}
	return b.String(), args, nil
}

// cutInKeyword returns the SQL before its trailing IN or NOT IN keyword, and the operator replacing it
// with an array: "= ANY" or "<> ALL".
func cutInKeyword(sql string) (prefix, op string, ok bool) {
	s, ok := cutKeywordSuffix(sql, "IN")
	if !ok {
		return "", "", false
	}
	if s, ok := cutKeywordSuffix(s, "NOT"); ok {
		return s, "<> ALL", true
	}
	return s, "= ANY", true
}

// cutKeywordSuffix returns the SQL before the keyword ending it, ignoring the trailing spaces and the case.
func cutKeywordSuffix(sql, keyword string) (string, bool) {
	s := strings.TrimRight(sql, " \t\r\n")
	i := len(s) - len(keyword)
	if i < 0 || !strings.EqualFold(s[i:], keyword) || i > 0 && isIdentChar(s[i-1]) {
		return "", false
	}
	return s[:i], true
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isIdentChar(c byte) bool {
	return c == '_' || c == '$' || isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// orBackground returns ctx, or context.Background() when it is nil.
func orBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
