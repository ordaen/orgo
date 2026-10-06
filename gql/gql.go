// Package gql helps GraphQL resolvers served with gin: request values, scoped queries of repositories
// with optional filters, sorting and pagination, and field updates recording their changes.
package gql

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ordaen/orgo/changes"
	"github.com/ordaen/orgo/events"
	"github.com/ordaen/orgo/sessions"
	"github.com/ordaen/orgo/types"
)

type (
	GinContextKey string
	Changes       = changes.Changes
	Change        = changes.Change
)

const (
	GinCtxKey GinContextKey = "GinContextKey"
)

// GinContext returns the gin context stored in ctx with GinCtxKey
func GinContext(ctx context.Context) (*gin.Context, error) {
	ginContext := ctx.Value(GinCtxKey)
	if ginContext == nil {
		return nil, fmt.Errorf("could not retrieve gin.Context")
	}

	gc, ok := ginContext.(*gin.Context)
	if !ok {
		return nil, fmt.Errorf("gin.Context has wrong type")
	}
	return gc, nil
}

// GetRequestEnv returns the request environment of the gin context in ctx, see GetGinEnv
func GetRequestEnv(ctx context.Context) map[string]any {
	gc, err := GinContext(ctx)
	if err != nil {
		return nil
	}
	return GetGinEnv(gc)
}

// Redacted replaces the values of the headers with credentials in the request environment.
const Redacted = "[REDACTED]"

// redactedHeaders are the headers with credentials, their values are not in the request environment.
var redactedHeaders = []string{"Authorization", "Proxy-Authorization", "Cookie", "Set-Cookie", "X-Api-Key", "X-Auth-Token"}

// GetGinEnv returns the user agent and the headers of the request, with the credentials redacted
func GetGinEnv(c *gin.Context) map[string]any {
	res := map[string]any{
		"User Agent": c.Request.UserAgent(),
	}
	for k, v := range c.Request.Header {
		if slices.Contains(redactedHeaders, http.CanonicalHeaderKey(k)) {
			res[k] = Redacted
			continue
		}
		res[k] = v
	}
	return res
}

// EnvToString formats the environment as "key: value" lines sorted by key, without the excluded keys
func EnvToString(env map[string]any, exclude []string) string {
	if env == nil {
		return ""
	}

	var buf strings.Builder
	keys := make([]string, 0, len(env))
	for k := range env {
		if !slices.Contains(exclude, k) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		buf.WriteString(k)
		buf.WriteString(": ")
		fmt.Fprintf(&buf, "%v", env[k])
		buf.WriteRune('\n')
	}
	return buf.String()
}

// GetIP returns the client IP of the request
func GetIP(ctx context.Context) string {
	gc, err := GinContext(ctx)
	if err != nil {
		return ""
	}
	return gc.ClientIP()
}

// GetUser returns the user of the session of the request, an empty user without a session
func GetUser(ctx context.Context) *types.User {
	return GetSession(ctx).User()
}

// GetSession returns the session stored in the gin context with the key "session", an empty session without it
func GetSession(ctx context.Context) *sessions.Session {
	gc, err := GinContext(ctx)
	if err != nil {
		return &sessions.Session{}
	}
	if v, ok := gc.Value("session").(*sessions.Session); ok && v != nil {
		return v
	}
	return &sessions.Session{}
}

// GetIDFromEvent returns the ID of the event, see events.Event.ID, or its string data
func GetIDFromEvent(e events.Event) string {
	if id := e.ID(); id != "" {
		return id
	}
	if s, ok := e.Data.(string); ok {
		return s
	}
	return ""
}
