package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/validator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorsAddError(t *testing.T) {
	v := &validator.Validator{}
	v.Present("", "name")
	v.Present("", "email")

	var e Errors
	e.AddError(nil)
	assert.False(t, e.HasErrors())
	assert.Equal(t, 200, e.StatusCode())

	e.AddError(fmt.Errorf("find: %w", pg.ErrRecordNotFound))
	e.AddError(fmt.Errorf("wrapped: %w", ErrorForbidden("no access")))
	e.AddError(Errors{Errors: []Error{ErrorBadRequest("a"), ErrorBadRequest("b")}})
	e.AddError(v.Verify())
	e.AddError(errors.New("boom"))

	require.True(t, e.HasErrors())
	assert.Equal(t, 404, e.StatusCode())
	assert.Equal(t, []Error{
		ErrorRecordNotFound,
		ErrorForbidden("no access"),
		ErrorBadRequest("a"),
		ErrorBadRequest("b"),
		{Status: "422", Title: "Invalid Attribute", Detail: "name required"},
		{Status: "422", Title: "Invalid Attribute", Detail: "email required"},
		ErrorInternal("boom"),
	}, e.Errors)
}

func TestErrorStatusCode(t *testing.T) {
	assert.Equal(t, 422, ErrorInvalidAttribute("name", "taken").StatusCode())
	assert.Equal(t, 500, Error{Status: "200"}.StatusCode())
	assert.Equal(t, 500, Error{}.StatusCode())
}

func TestErrorsJSON(t *testing.T) {
	b, err := json.Marshal(Errors{Errors: []Error{ErrorInvalidAttribute("name", "taken")}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"errors":[{"status":"422","attribute":"name","title":"Invalid Attribute","detail":"taken"}]}`, string(b))
	b, err = json.Marshal(Errors{})
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(b))
}
