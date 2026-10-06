package gql

const (
	DefaultPage  = 1
	DefaultLimit = 10
	MaxLimit     = 100
)

// Pagination is a struct that contains the pagination information
type Pagination struct {
	Page        int    `json:"page"`
	Limit       int    `json:"limit"`
	Total       int    `json:"total"`
	HasNext     bool   `json:"hasNext"`
	HasPrevious bool   `json:"hasPrevious"`
	NextCursor  string `json:"nextCursor"`
}

// Offset returns the offset for the pagination
func (p *Pagination) Offset() int {
	return (p.Page - 1) * p.Limit
}

// NewPagination creates a new pagination with the given page and limit, DefaultPage and DefaultLimit
// when they are not given or not positive, the limit is at most MaxLimit
func NewPagination(page *int, limit *int) Pagination {
	res := Pagination{
		Page:  DefaultPage,
		Limit: clampLimit(limit),
	}
	if page != nil && *page > 0 {
		res.Page = *page
	}
	return res
}

// clampLimit returns the limit, DefaultLimit when it is not given or not positive, at most MaxLimit.
func clampLimit(limit *int) int {
	if limit == nil || *limit <= 0 {
		return DefaultLimit
	}
	return min(*limit, MaxLimit)
}

// PaginatedResponse is a struct that contains the paginated response for a list of items
type PaginatedResponse[T any] struct {
	Items      []T        `json:"items"`
	Pagination Pagination `json:"pagination"`
}
