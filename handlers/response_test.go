package handlers

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseJSON(t *testing.T) {
	rec := &jsonItem{Name: "n"}
	rec.ID = 5
	for _, tc := range []struct {
		resp func() *Response
		want string
	}{
		{func() *Response { return &Response{} }, `{}`},
		{func() *Response { return &Response{Data: true} }, `{"data":true}`},
		{func() *Response { return &Response{Data: false} }, `{"data":false}`},
		{func() *Response { return &Response{Meta: &MetaData{Total: 1}} }, `{"meta":{"total":1,"limit":0,"offset":0}}`},
		{func() *Response {
			r := &Response{Data: []*jsonItem{rec}, Meta: &MetaData{Total: 1, Limit: 30}}
			r.SetFields([]string{"name"})
			return r
		}, `{"data":[{"id":"5","name":"n"}],"meta":{"total":1,"limit":30,"offset":0}}`},
		{func() *Response {
			r := &Response{Data: rec, Meta: &MetaData{}}
			r.AddError(errors.New("boom"))
			return r
		}, `{"errors":[{"status":"500","title":"Internal Server Error","detail":"boom"}]}`},
	} {
		r := tc.resp()
		b, err := json.Marshal(r)
		require.NoError(t, err)
		assert.Equal(t, tc.want, string(b))
		b, err = json.Marshal(*r)
		require.NoError(t, err)
		assert.Equal(t, tc.want, string(b), "a value")
	}
}

func TestResponseJSONError(t *testing.T) {
	_, err := json.Marshal(&Response{Data: func() {}})
	assert.Error(t, err)
}
