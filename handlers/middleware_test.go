package handlers

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/sessions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clearBlockedIPs removes the blocked addresses.
func clearBlockedIPs(t *testing.T) {
	t.Helper()
	require.NoError(t, pg.ClearTables("blocked_ips"))
	require.NoError(t, sessions.BlockedIPs.Reload())
}

func TestCheckIP(t *testing.T) {
	clearBlockedIPs(t)
	t.Cleanup(func() { clearBlockedIPs(t) })
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/", CheckIP, func(g *gin.Context) { g.String(200, "ok") })

	// httptest requests come from 192.0.2.1
	r := serve(t, engine, "GET", "/", "")
	assert.Equal(t, 200, r.code)

	_, err := sessions.BlockIP("192.0.2.0/24", 600, "test")
	require.NoError(t, err)
	r = serve(t, engine, "GET", "/", "")
	assert.Equal(t, 403, r.code)
	assert.Contains(t, r.body, `"detail":"this IP address is temporary blocked, you can retry in `)
	assert.Contains(t, r.body, ` minutes"`)

	_, err = sessions.BlockIP("192.0.2.1", 0, "test")
	require.NoError(t, err)
	r = serve(t, engine, "GET", "/", "")
	assert.Equal(t, 403, r.code)
	assert.JSONEq(t, `{"errors":[{"status":"403","title":"Forbidden","detail":"access from this IP address forbidden"}]}`, r.body)
}

func TestDurationToHuman(t *testing.T) {
	assert.Equal(t, "5 seconds", durationToHuman(5))
	assert.Equal(t, "119 seconds", durationToHuman(119))
	assert.Equal(t, "2 minutes", durationToHuman(120))
	assert.Equal(t, "10 minutes", durationToHuman(659))
}

func TestCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		origins []string
		origin  string
		allowed bool
	}{
		{nil, "https://a.example", true},
		{[]string{"https://a.example"}, "https://a.example", true},
		{[]string{"https://a.example"}, "https://b.example", false},
		{nil, "", false},
	} {
		engine := gin.New()
		engine.Use(CORS(tc.origins...))
		engine.GET("/", func(g *gin.Context) { g.String(200, "ok") })
		engine.OPTIONS("/", func(g *gin.Context) { g.String(500, "not reached") })

		r := serve(t, engine, "GET", "/", "", "Origin", tc.origin)
		assert.Equal(t, 200, r.code)
		assert.Equal(t, "ok", r.body)
		if tc.allowed {
			assert.Equal(t, tc.origin, r.header.Get("Access-Control-Allow-Origin"))
			assert.Equal(t, "GET, OPTIONS", r.header.Get("Access-Control-Allow-Methods"))
			assert.Equal(t, []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers"}, r.header.Values("Vary"))
		} else {
			assert.Empty(t, r.header.Get("Access-Control-Allow-Origin"), tc.origin)
		}

		r = serve(t, engine, http.MethodOptions, "/", "", "Origin", tc.origin)
		assert.Equal(t, 200, r.code, "the preflight is answered")
		assert.Empty(t, r.body)
	}
}
