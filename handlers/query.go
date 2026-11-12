package handlers

import (
	"fmt"
	"maps"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
)

// DefaultLimit is the limit of the list queries without one.
const DefaultLimit = 30

// Query is the list query of a request, parsed from its URL query by ParseQuery:
//
//	ids=1,2,3                    records by ID
//	limit=30&offset=60           page
//	fields=name,email            fields of the response
//	sort=name,-created           order, - for descending
//	filter[status]=active        status = 'active'
//	filter[active]=true          active IS TRUE, false for IS NOT TRUE
//	filter[status]=new,active    status IN ('new', 'active')
//	query[name]=jo&query[email]=jo  name or email contains jo, case insensitive
//	time[from]=...&time[to]=...  created in [from, to), RFC 3339 times
//	time[column]=updated         the column of time, created by default
//
// The other keys are left to the handlers. ApplyQuery checks the columns against the model.
type Query struct {
	IDs     []string
	Limit   int
	Offset  int
	Fields  []string
	Sort    []SortColumn
	Filters map[string]string
	Queries map[string]string
	Time    *TimeRange
}

// SortColumn is a column of the order of a Query.
type SortColumn struct {
	Column string
	Desc   bool
}

// String returns the column followed by DESC when descending, like "name" or "created DESC".
func (s SortColumn) String() string {
	if s.Desc {
		return s.Column + " DESC"
	}
	return s.Column
}

// Expr returns the ORDER BY expression of the column, quoted.
func (s SortColumn) Expr() string {
	if s.Desc {
		return pg.QuoteIdent(s.Column) + " DESC"
	}
	return pg.QuoteIdent(s.Column)
}

// TimeRange is the time range of a Query: Column from From, included, to To, excluded. A nil bound is not checked.
type TimeRange struct {
	From   *time.Time
	To     *time.Time
	Column string
}

// ModelWithCustomQueries is a model with its own conditions for some query[key] values, like an IP address
// matching the networks containing it. A condition is raw SQL with ? placeholders for args, like pg.ModelQuery.Where.
// When ok is false the default condition is used: the key column contains the value, case insensitive.
type ModelWithCustomQueries interface {
	CustomQueries(key, val string) (where string, args []any, ok bool)
}

// ModelWithCustomSort is a model with its own ORDER BY expression for some sorts, like the IP addresses ordered
// by their mask length after their address. The sort columns are checked, the default sort of the model is
// not. When ok is false the columns are ordered as they are.
type ModelWithCustomSort interface {
	CustomSort(sort []SortColumn) (order string, ok bool)
}

// ModelWithSortBy is a model with its default ORDER BY expression, used when the query has no sort.
type ModelWithSortBy interface {
	SortBy() string
}

// ParseQuery parses the list query of values, see Query. It returns an ErrorBadRequest for invalid values.
// Of the keys given more than once the first value is used.
func ParseQuery(values url.Values) (*Query, error) {
	q := &Query{Filters: map[string]string{}, Queries: map[string]string{}}
	for key, vals := range values {
		if len(vals) == 0 {
			continue
		}
		val := vals[0]
		var err error
		switch {
		case key == "ids":
			q.IDs = splitList(val)
		case key == "limit":
			q.Limit, err = parseCount(key, val)
		case key == "offset":
			q.Offset, err = parseCount(key, val)
		case key == "fields":
			q.Fields = splitList(val)
		case key == "sort":
			q.Sort, err = parseSort(val)
		case strings.HasPrefix(key, "filter[") && strings.HasSuffix(key, "]"):
			q.Filters[key[len("filter["):len(key)-1]] = val
		case strings.HasPrefix(key, "query[") && strings.HasSuffix(key, "]"):
			q.Queries[key[len("query["):len(key)-1]] = val
		case strings.HasPrefix(key, "time[") && strings.HasSuffix(key, "]"):
			err = q.parseTime(key[len("time["):len(key)-1], val)
		}
		if err != nil {
			return nil, err
		}
	}
	return q, nil
}

// splitList returns the comma separated values of s, trimmed, without the empty ones.
func splitList(s string) []string {
	var res []string
	for v := range strings.SplitSeq(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			res = append(res, v)
		}
	}
	return res
}

// parseCount returns the non-negative number of val, 0 for an empty val.
func parseCount(key, val string) (int, error) {
	if val == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(val)
	if err != nil || n < 0 {
		return 0, ErrorBadRequest(fmt.Sprintf("invalid %s %q", key, val))
	}
	return n, nil
}

// parseSort returns the sort columns of val, like "name,-created".
func parseSort(val string) ([]SortColumn, error) {
	var res []SortColumn
	for _, v := range splitList(val) {
		col := SortColumn{Column: v}
		if c, ok := strings.CutPrefix(v, "-"); ok {
			col = SortColumn{Column: c, Desc: true}
		}
		if col.Column == "" {
			return nil, ErrorBadRequest(fmt.Sprintf("invalid sort %q", val))
		}
		res = append(res, col)
	}
	return res, nil
}

// parseTime sets the key of the time range to val. An empty val is ignored.
func (q *Query) parseTime(key, val string) error {
	if val == "" {
		return nil
	}
	if q.Time == nil {
		q.Time = &TimeRange{Column: "created"}
	}
	switch key {
	case "from", "to":
		t, err := time.Parse(time.RFC3339, val)
		if err != nil {
			return ErrorBadRequest(fmt.Sprintf("invalid time[%s] %q", key, val))
		}
		if key == "from" {
			q.Time.From = &t
		} else {
			q.Time.To = &t
		}
	case "column":
		q.Time.Column = val
	default:
		return ErrorBadRequest(fmt.Sprintf("invalid time key %q", key))
	}
	return nil
}

// ApplyQuery returns q with the conditions, order and page of args. It returns an ErrorBadRequest when a column
// of args is not a column of T or an ID is invalid. Without sort the records are ordered by the SortBy of T
// when it has it, and by the ID last when it is not in the sort, so the pages do not overlap.
func ApplyQuery[T model.Model](q *pg.ModelQuery[T], args *Query) (*pg.ModelQuery[T], error) {
	m := newModel[T]()
	if len(args.IDs) > 0 {
		ids, err := parseIDs(m, args.IDs)
		if err != nil {
			return nil, err
		}
		q = q.Where(`"id" IN ?`, ids)
	}
	for _, key := range slices.Sorted(maps.Keys(args.Filters)) {
		where, val, err := filterCond[T](key, args.Filters[key])
		if err != nil {
			return nil, err
		}
		if val == nil {
			q = q.Where(where)
		} else {
			q = q.Where(where, val)
		}
	}
	if where, vals, err := searchCond(m, args.Queries); err != nil {
		return nil, err
	} else if where != "" {
		q = q.Where(where, vals...)
	}
	if t := args.Time; t != nil && (t.From != nil || t.To != nil) {
		if !pg.HasColumn[T](t.Column) {
			return nil, unknownColumn("time", t.Column)
		}
		col := pg.QuoteIdent(t.Column)
		if t.From != nil {
			q = q.Where(col+" >= ?", *t.From)
		}
		if t.To != nil {
			q = q.Where(col+" < ?", *t.To)
		}
	}
	order, err := orderOf(m, args.Sort)
	if err != nil {
		return nil, err
	}
	return q.Order(order).Limit(args.Limit).Offset(args.Offset), nil
}

// newModel returns a new T, a pointer to a model struct.
func newModel[T model.Model]() T {
	t := reflect.TypeFor[T]()
	if t.Kind() != reflect.Pointer {
		var zero T
		return zero
	}
	return reflect.New(t.Elem()).Interface().(T)
}

// parseIDs returns the IN list of the ids, as numbers when the ID of m is a number, as they are otherwise.
func parseIDs(m model.Model, ids []string) (pg.InList, error) {
	switch reflect.ValueOf(m.GetID()).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		res := make([]int64, 0, len(ids))
		for _, v := range ids {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return pg.InList{}, ErrorBadRequest(fmt.Sprintf("invalid id %q", v))
			}
			res = append(res, n)
		}
		return pg.In(res), nil
	}
	return pg.In(ids), nil
}

// filterCond returns the condition of filter[key]=val and its value, nil when it has none.
func filterCond[T model.Model](key, val string) (string, any, error) {
	if !pg.HasColumn[T](key) {
		return "", nil, unknownColumn("filter", key)
	}
	col := pg.QuoteIdent(key)
	switch {
	case val == "true":
		return col + " IS TRUE", nil, nil
	case val == "false":
		return col + " IS NOT TRUE", nil, nil
	case strings.Contains(val, ","):
		return col + " IN ?", pg.In(strings.Split(val, ",")), nil
	}
	return col + " = ?", val, nil
}

// searchCond returns the conditions of the queries joined with OR, "" without queries with a value.
func searchCond[T model.Model](m T, queries map[string]string) (string, []any, error) {
	custom, _ := any(m).(ModelWithCustomQueries)
	var (
		conds []string
		args  []any
	)
	for _, key := range slices.Sorted(maps.Keys(queries)) {
		val := queries[key]
		if val == "" {
			continue
		}
		if custom != nil {
			if where, vals, ok := custom.CustomQueries(key, val); ok {
				conds = append(conds, "("+where+")")
				args = append(args, vals...)
				continue
			}
		}
		if !pg.HasColumn[T](key) {
			return "", nil, unknownColumn("query", key)
		}
		conds = append(conds, pg.QuoteIdent(key)+"::text ILIKE ?")
		args = append(args, "%"+likeEscaper.Replace(val)+"%")
	}
	return strings.Join(conds, " OR "), args, nil
}

// orderOf returns the ORDER BY expression of the sort, or of the SortBy of m without sort, ending with the ID.
func orderOf[T model.Model](m T, sort []SortColumn) (string, error) {
	if len(sort) > 0 {
		for _, s := range sort {
			if !pg.HasColumn[T](s.Column) {
				return "", unknownColumn("sort", s.Column)
			}
		}
	} else if sorter, ok := any(m).(ModelWithSortBy); ok {
		sortBy := sorter.SortBy()
		parsed, ok := parseSortBy(sortBy)
		if !ok {
			// not a list of columns, an expression used as it is
			return sortBy, nil
		}
		sort = parsed
	}
	var order string
	if custom, ok := any(m).(ModelWithCustomSort); ok && len(sort) > 0 {
		order, _ = custom.CustomSort(sort)
	}
	if order == "" {
		exprs := make([]string, 0, len(sort))
		for _, s := range sort {
			exprs = append(exprs, s.Expr())
		}
		order = strings.Join(exprs, ", ")
	}
	if pg.HasColumn[T]("id") && !slices.ContainsFunc(sort, func(s SortColumn) bool { return s.Column == "id" }) {
		if order != "" {
			order += ", "
		}
		order += `"id"`
	}
	return order, nil
}

// parseSortBy returns the columns of a SortBy expression made of columns optionally followed by ASC or DESC,
// like "name, created DESC". ok is false for the other expressions.
func parseSortBy(s string) ([]SortColumn, bool) {
	var res []SortColumn
	for part := range strings.SplitSeq(s, ",") {
		fields := strings.Fields(part)
		if len(fields) == 0 || len(fields) > 2 || !isIdent(fields[0]) {
			return nil, false
		}
		col := SortColumn{Column: fields[0]}
		if len(fields) == 2 {
			switch strings.ToUpper(fields[1]) {
			case "ASC":
			case "DESC":
				col.Desc = true
			default:
				return nil, false
			}
		}
		res = append(res, col)
	}
	return res, len(res) > 0
}

// isIdent reports whether s is a plain lowercase SQL identifier.
func isIdent(s string) bool {
	for i, c := range s {
		if c != '_' && (c < 'a' || c > 'z') && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return s != ""
}

func unknownColumn(param, column string) error {
	return ErrorBadRequest(fmt.Sprintf("invalid %s column %q", param, column))
}

// likeEscaper escapes the LIKE wildcards and the escape character of a value.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
