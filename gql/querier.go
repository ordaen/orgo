package gql

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/ordaen/orgo/logger"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"github.com/ordaen/orgo/utils"
)

// ErrNotFound is returned when the record is not found in the scopes of the querier.
var ErrNotFound = errors.New("record not found")

// QueryScope is a condition of all queries of a Querier
type QueryScope struct {
	Where string
	Args  []any
}

// NewQuerier returns a querier of the repository records
func NewQuerier[T model.Model](store repo.Repository[T]) *Querier[T] {
	return &Querier[T]{store: store}
}

// Querier queries the records of a repository for the resolvers, in its scopes. It can be shared,
// Where returns a new querier.
type Querier[T model.Model] struct {
	store       repo.Repository[T]
	whereScopes []QueryScope
}

// Where returns a copy of the querier with the condition added to its scopes, the querier is not changed
//
//	q.Where("owner_id = ?", user.ID).Find(ctx, id)
func (r *Querier[T]) Where(where string, args ...any) *Querier[T] {
	return &Querier[T]{store: r.store, whereScopes: append(slices.Clip(r.whereScopes), QueryScope{Where: where, Args: args})}
}

// query starts a query of the records in the scopes, running with ctx.
func (r *Querier[T]) query(ctx context.Context) *pg.ModelQuery[T] {
	query := r.store.WithContext(ctx).Query()
	for _, v := range r.whereScopes {
		query = query.Where(v.Where, v.Args...)
	}
	return query
}

// Find returns the record with the id in the scopes, or ErrNotFound
func (r *Querier[T]) Find(ctx context.Context, id model.ModelID) (T, error) {
	var empty T
	rec, err := r.query(ctx).Where("id = ?", id).First()
	if errors.Is(err, pg.ErrRecordNotFound) || (err == nil && !rec.GetID().Valid()) {
		return empty, ErrNotFound
	}
	if err != nil {
		return empty, err
	}
	return rec, nil
}

// Delete deletes the record with the id in the scopes and logs the deletion by the user of the request
func (r *Querier[T]) Delete(ctx context.Context, id model.ModelID) (T, error) {
	var empty T
	rec, err := r.Find(ctx, id)
	if err != nil {
		return empty, err
	}
	if err := r.store.WithContext(ctx).Delete(rec); err != nil {
		logger.Error("delete").WithUser(GetUser(ctx)).WithModel(rec).WithMessage(err.Error()).Create()
		return empty, err
	}
	logger.Info("delete").WithUser(GetUser(ctx)).WithModel(rec).Create()
	return rec, nil
}

// FindExec returns the record with the id in the scopes after calling funcs with it, the first error is returned
func (r *Querier[T]) FindExec(ctx context.Context, id model.ModelID, funcs ...func(T) error) (T, error) {
	var empty T
	rec, err := r.Find(ctx, id)
	if err != nil {
		return empty, err
	}
	for _, v := range funcs {
		if err := v(rec); err != nil {
			return empty, err
		}
	}
	return rec, nil
}

// FindMany returns the records in the scopes matching the queries. The limit is DefaultLimit when it is not
// given, at most MaxLimit. sort is validated, see Sort.
func (r *Querier[T]) FindMany(ctx context.Context, limit *int, offset *int, sort *string, queries ...Where) ([]T, error) {
	query, err := r.buildQuery(ctx, sort, queries)
	if err != nil {
		return nil, err
	}
	query = query.Limit(clampLimit(limit))
	if offset != nil && *offset > 0 {
		query = query.Offset(*offset)
	}
	return query.Select()
}

// FindManyPaginated returns the page of the records in the scopes matching the queries, see NewPagination.
// sort is validated, see Sort.
func (r *Querier[T]) FindManyPaginated(ctx context.Context, limit *int, page *int, sort *string, queries ...Where) (*PaginatedResponse[T], error) {
	query, err := r.buildQuery(ctx, sort, queries)
	if err != nil {
		return nil, err
	}

	pagination := NewPagination(page, limit)
	offset := pagination.Offset()
	pagination.Total, err = query.Count()
	if err != nil {
		return nil, err
	}
	res, err := query.Limit(pagination.Limit).Offset(offset).Select()
	if err != nil {
		return nil, err
	}

	if pagination.Total > offset+pagination.Limit {
		pagination.HasNext = true
		if len(res) > 0 {
			pagination.NextCursor = res[len(res)-1].GetID().String()
		}
	}
	pagination.HasPrevious = offset > 0
	return &PaginatedResponse[T]{Items: res, Pagination: pagination}, nil
}

// buildQuery returns the query of the records in the scopes matching the queries, sorted by sort.
func (r *Querier[T]) buildQuery(ctx context.Context, sort *string, queries []Where) (*pg.ModelQuery[T], error) {
	order, err := Sort[T](sort)
	if err != nil {
		return nil, err
	}
	query := r.query(ctx).Order(order)
	for _, v := range queries {
		switch {
		case v.fixed != "":
			query = query.Where(v.fixed)
		case v.Applicable():
			query = query.Where(v.key, v.args...)
		}
	}
	return query, nil
}

type modelWithSortBy interface{ SortBy() string }

// Sort returns the ORDER BY expression of sort given by a client: comma separated columns of T, each optionally
// followed by asc or desc, like "name, created desc". It returns an error for the other values, they are not
// SQL. Without sort it returns the SortBy of T when it has it, or "".
func Sort[T model.Model](sort *string) (string, error) {
	if sort == nil || strings.TrimSpace(*sort) == "" {
		if v, ok := any(utils.TypeCreate[T]()).(modelWithSortBy); ok {
			return v.SortBy(), nil
		}
		return "", nil
	}
	var parts []string
	for item := range strings.SplitSeq(*sort, ",") {
		fields := strings.Fields(item)
		if len(fields) == 0 || len(fields) > 2 || !pg.HasColumn[T](fields[0]) {
			return "", fmt.Errorf("invalid sort %q", *sort)
		}
		part := pg.QuoteIdent(fields[0])
		if len(fields) == 2 {
			dir := strings.ToUpper(fields[1])
			if dir != "ASC" && dir != "DESC" {
				return "", fmt.Errorf("invalid sort %q", *sort)
			}
			part += " " + dir
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", "), nil
}

// likeEscaper escapes the LIKE wildcards and the escape character of a value.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func mask[T string | *string](mask string, val T) string {
	switch v := any(val).(type) {
	case string:
		return fmt.Sprintf(mask, likeEscaper.Replace(v))
	case *string:
		if v != nil {
			return fmt.Sprintf(mask, likeEscaper.Replace(*v))
		}
	}
	return ""
}

// Like returns the LIKE pattern of the values containing val, the wildcards in val are matched literally.
// It returns "" for a nil val, so the condition using it is not applied.
//
//	gql.Cond("name ILIKE ?", gql.Like(name))
func Like[T string | *string](val T) string {
	return mask("%%%s%%", val)
}

// CondTime returns the condition of the field between from and to, a missing bound is not checked
func CondTime(field string, from *time.Time, to *time.Time) Where {
	switch {
	case from != nil && to != nil:
		return Where{key: fmt.Sprintf("%s between ? and ?", field), args: []any{*from, *to}}
	case from != nil:
		return Where{key: fmt.Sprintf("%s >= ?", field), args: []any{*from}}
	case to != nil:
		return Where{key: fmt.Sprintf("%s <= ?", field), args: []any{*to}}
	}
	return Where{}
}

// Cond returns an optional condition, applied only when all its values are set: not nil and not zero.
// The optional arguments of the resolvers are pointers, so a pointer to a zero value, like to false, is applied.
//
//	gql.Cond("active = ?", active) // active *bool
func Cond(query string, values ...any) Where {
	return Where{key: query, args: values}
}

// FixedCond returns a condition without values, always applied. It is raw SQL, not for client values.
func FixedCond(query string) Where {
	return Where{fixed: query}
}

// Where is a condition of FindMany and FindManyPaginated
type Where struct {
	fixed string
	key   string
	args  []any
}

// Applicable reports whether the condition has values and all are set, see Cond
func (a Where) Applicable() bool {
	return len(a.args) > 0 && !slices.ContainsFunc(a.args, isUnset)
}

// isUnset reports whether the value is nil, a nil pointer or another zero value.
func isUnset(v any) bool {
	rv := reflect.ValueOf(v)
	return !rv.IsValid() || rv.IsZero()
}
