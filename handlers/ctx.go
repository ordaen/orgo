package handlers

import (
	"context"
	"encoding/json"
	"io"

	"github.com/gin-gonic/gin"
	"github.com/ordaen/orgo/logs"
	"github.com/ordaen/orgo/sessions"
	"github.com/ordaen/orgo/types"
)

// Ctx is the request of an action: the gin context, the response the action builds and the scopes of the queries
// of the records. The response is rendered as JSON after the action, unless the action wrote the response itself,
// like a file download with Gin.Data.
type Ctx struct {
	Gin *gin.Context
	// ResponseCode is the HTTP status of the response, 200 by default. With errors a status under 400 is
	// replaced by the status of the first error.
	ResponseCode int
	Resp         Response

	controller *Controller
	scopes     []whereScope
}

type whereScope struct {
	where string
	args  []any
}

// Context returns the context of the request.
func (c *Ctx) Context() context.Context {
	return c.Gin.Request.Context()
}

// Param returns the value of the URL parameter, like "id" of "/users/:id".
func (c *Ctx) Param(name string) string {
	return c.Gin.Param(name)
}

// QueryParam returns the value of the URL query key, the first one when it is given several times.
func (c *Ctx) QueryParam(key string) string {
	return c.Gin.Query(key)
}

// IP returns the client IP of the request.
func (c *Ctx) IP() string {
	return c.Gin.ClientIP()
}

// Session returns the session stored in the gin context with the key "session" by the authentication
// middleware, like gql.GetSession, an empty session without it.
func (c *Ctx) Session() *sessions.Session {
	if s, ok := c.Gin.Value("session").(*sessions.Session); ok && s != nil {
		return s
	}
	return &sessions.Session{}
}

// User returns the user of the session with the client IP of the request, a user named Undefined without
// a session. The records written by the actions are logged with it.
func (c *Ctx) User() *types.User {
	s := c.Session()
	if !s.UserID.Valid() {
		return &types.User{Name: "Undefined", IP: c.IP()}
	}
	u := s.User()
	u.IP = c.IP()
	return u
}

// LoadJSONRequest decodes the JSON body of the request into dst. When it fails it adds an ErrorBadRequest to the
// response and returns false:
//
//	req := new(loginRequest)
//	if !c.LoadJSONRequest(req) {
//		return
//	}
func (c *Ctx) LoadJSONRequest(dst any) bool {
	body, err := c.readBody()
	if err != nil {
		c.AddError(err)
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		c.AddError(ErrorBadRequest("invalid JSON request: " + err.Error()))
		return false
	}
	return true
}

// readBody returns the body of the request.
func (c *Ctx) readBody() ([]byte, error) {
	if c.Gin.Request.Body == nil {
		return nil, nil
	}
	defer func() { _ = c.Gin.Request.Body.Close() }()
	b, err := io.ReadAll(c.Gin.Request.Body)
	if err != nil {
		return nil, ErrorBadRequest("cannot read the request body: " + err.Error())
	}
	return b, nil
}

// AddWhereScope adds a condition to the queries of the records by the actions: GetRecords, FetchRecord and the
// default actions of a Resource. It is raw SQL with ? placeholders for args, like pg.ModelQuery.Where.
//
//	c.AddWhereScope("masklen(ip) = ?", c.QueryParam("mask"))
func (c *Ctx) AddWhereScope(where string, args ...any) *Ctx {
	c.scopes = append(c.scopes, whereScope{where: where, args: args})
	return c
}

// Args returns the list query of the request, see ParseQuery. The limit is DefaultLimit when it is not given,
// at most the MaxRecords of the controller when it is set.
func (c *Ctx) Args() (*Query, error) {
	q, err := ParseQuery(c.Gin.Request.URL.Query())
	if err != nil {
		return nil, err
	}
	if q.Limit == 0 {
		q.Limit = DefaultLimit
	}
	if c.controller != nil && c.controller.MaxRecords > 0 {
		q.Limit = min(q.Limit, c.controller.MaxRecords)
	}
	return q, nil
}

// AddError adds the error to the response, see Errors.AddError. A nil error is ignored.
func (c *Ctx) AddError(err error) {
	c.Resp.AddError(err)
}

// SetData sets the data of the response.
func (c *Ctx) SetData(data any) {
	c.Resp.Data = data
}

// SetDataOrError sets the data of the response, or adds the error when it is not nil.
func (c *Ctx) SetDataOrError(data any, err error) {
	if err != nil {
		c.AddError(err)
		return
	}
	c.SetData(data)
}

// SetMeta sets the meta data of the response.
func (c *Ctx) SetMeta(meta *MetaData) {
	c.Resp.Meta = meta
}

// SetFields sets the fields of the data of the response, see EncodeData.
func (c *Ctx) SetFields(fields []string) {
	c.Resp.SetFields(fields)
}

const jsonContentType = "application/json; charset=utf-8"

// response returns the status and the JSON of the response, no JSON for 204. ok is false when the action wrote
// the response or aborted the request.
func (c *Ctx) response() (code int, b []byte, ok bool) {
	if c.Gin.Writer.Written() || c.Gin.IsAborted() {
		return 0, nil, false
	}
	code = c.ResponseCode
	switch {
	case c.Resp.HasErrors():
		if code < 400 {
			code = c.Resp.StatusCode()
		}
	case code == 0:
		code = 200
	}
	if code == 204 {
		return code, nil, true
	}
	b, err := json.Marshal(&c.Resp)
	if err != nil {
		logs.Error(err, "handlers: encode the response")
		code, b = errorJSON(ErrorInternal("cannot encode the response"))
	}
	return code, b, true
}

// RenderError writes the JSON response of the error and aborts the request, for the middlewares:
//
//	if !loggedIn(g) {
//		handlers.RenderError(g, handlers.ErrorUnauthorized)
//		return
//	}
func RenderError(g *gin.Context, err error) {
	code, b := errorJSON(err)
	g.Data(code, jsonContentType, b)
	g.Abort()
}

// errorJSON returns the status and the JSON response of the error.
func errorJSON(err error) (int, []byte) {
	var r Response
	r.AddError(err)
	if !r.HasErrors() {
		r.AddError(ErrorInternal("unknown error"))
	}
	b, _ := json.Marshal(&r)
	return r.StatusCode(), b
}
