package handlers

import (
	"fmt"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/ordaen/orgo/sessions"
)

// CheckIP is a middleware rejecting the requests from the blocked addresses, see sessions.BlockedIPs, with
// a 403 error telling when a temporary block ends.
func CheckIP(g *gin.Context) {
	left, blocked := sessions.BlockedIPs.IsBlocked(g.ClientIP())
	if !blocked {
		return
	}
	msg := "access from this IP address forbidden"
	if left > 0 {
		msg = "this IP address is temporary blocked, you can retry in " + durationToHuman(left)
	}
	RenderError(g, ErrorForbidden(msg))
}

// durationToHuman returns the seconds as seconds under 2 minutes, as minutes otherwise.
func durationToHuman(seconds int64) string {
	if seconds < 120 {
		return fmt.Sprintf("%d seconds", seconds)
	}
	return fmt.Sprintf("%d minutes", seconds/60)
}

// CORS returns a middleware allowing the GET requests from the origins, from any origin without origins.
// It answers the OPTIONS preflight requests with 200 and does not allow credentials.
func CORS(origins ...string) gin.HandlerFunc {
	return func(g *gin.Context) {
		origin := g.GetHeader("Origin")
		if origin != "" && (len(origins) == 0 || slices.Contains(origins, origin)) {
			h := g.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Origin, Accept")
			h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		}
		if g.Request.Method == http.MethodOptions {
			g.AbortWithStatus(http.StatusOK)
		}
	}
}
