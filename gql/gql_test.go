package gql

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ordaen/orgo/events"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/sessions"
	"github.com/ordaen/orgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ginCtx returns a context with a gin context of a request with the headers.
func ginCtx(t *testing.T, headers map[string]string) (context.Context, *gin.Context) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/graphql", nil)
	c.Request.RemoteAddr = "10.1.2.3:4567"
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	return context.WithValue(context.Background(), GinCtxKey, c), c
}

func TestGinContext(t *testing.T) {
	ctx, c := ginCtx(t, nil)
	got, err := GinContext(ctx)
	require.NoError(t, err)
	assert.Same(t, c, got)

	_, err = GinContext(context.Background())
	assert.EqualError(t, err, "could not retrieve gin.Context")
	_, err = GinContext(context.WithValue(context.Background(), GinCtxKey, "x"))
	assert.EqualError(t, err, "gin.Context has wrong type")
}

func TestRequestEnv(t *testing.T) {
	ctx, _ := ginCtx(t, map[string]string{
		"User-Agent":    "test-agent",
		"Authorization": "Bearer secret",
		"Cookie":        "session=secret",
		"X-Api-Key":     "secret",
		"Accept":        "application/json",
	})
	env := GetRequestEnv(ctx)
	assert.Equal(t, "test-agent", env["User Agent"])
	assert.Equal(t, Redacted, env["Authorization"])
	assert.Equal(t, Redacted, env["Cookie"])
	assert.Equal(t, Redacted, env["X-Api-Key"])
	assert.Equal(t, []string{"application/json"}, env["Accept"])

	s := EnvToString(env, []string{"User-Agent"})
	assert.NotContains(t, s, "secret")
	assert.Equal(t, "Accept: [application/json]\nAuthorization: [REDACTED]\nCookie: [REDACTED]\nUser Agent: test-agent\nX-Api-Key: [REDACTED]\n", s,
		"sorted by key, without the excluded keys")
	assert.Empty(t, EnvToString(nil, nil))
	assert.Nil(t, GetRequestEnv(context.Background()))
}

func TestGetIP(t *testing.T) {
	ctx, _ := ginCtx(t, nil)
	assert.Equal(t, "10.1.2.3", GetIP(ctx))
	assert.Empty(t, GetIP(context.Background()))
}

func TestGetSession(t *testing.T) {
	ctx, c := ginCtx(t, nil)
	assert.False(t, GetSession(ctx).ID.Valid())
	assert.Equal(t, &types.User{}, GetUser(ctx))

	s := &sessions.Session{Token: "token"}
	s.SetUserFields(&types.User{ID: model.ID(5), Name: "admin"})
	c.Set("session", s)
	assert.Same(t, s, GetSession(ctx))
	assert.Equal(t, model.ID(5), GetUser(ctx).ID)

	assert.NotNil(t, GetSession(context.Background()))
	assert.NotNil(t, GetUser(context.Background()))
}

func TestGetIDFromEvent(t *testing.T) {
	assert.Equal(t, "3", GetIDFromEvent(events.Event{Data: &item{ID: 3}}))
	assert.Equal(t, "4", GetIDFromEvent(events.Event{Data: model.ID(4)}))
	assert.Equal(t, "u-1", GetIDFromEvent(events.Event{Data: model.UUID("u-1")}))
	assert.Equal(t, "s", GetIDFromEvent(events.Event{Data: "s"}))
	assert.Empty(t, GetIDFromEvent(events.Event{Data: 5}))
}

func TestJSON(t *testing.T) {
	var j JSON
	require.NoError(t, j.UnmarshalGQL(`{"a":1}`))
	assert.Equal(t, JSON(`{"a":1}`), j)
	require.NoError(t, j.UnmarshalGQL([]byte(`[1,2]`)))
	assert.Equal(t, JSON(`[1,2]`), j)
	require.NoError(t, j.UnmarshalGQL(map[string]any{"b": true}), "a GraphQL input object")
	assert.Equal(t, JSON(`{"b":true}`), j)
	require.NoError(t, j.UnmarshalGQL([]any{"x", 1.5}))
	assert.Equal(t, JSON(`["x",1.5]`), j)
	assert.EqualError(t, j.UnmarshalGQL("{bad"), "invalid JSON")

	buf := new(bytes.Buffer)
	JSON(`{"a":1}`).MarshalGQL(buf)
	assert.Equal(t, `{"a":1}`, buf.String())
	buf.Reset()
	JSON(nil).MarshalGQL(buf)
	assert.Equal(t, "null", buf.String())
}
