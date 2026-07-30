// Package middleware holds cross-cutting Gin middleware: authentication,
// request ids and access logging.
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/bambam/hospital-middleware/internal/apierr"
	"github.com/bambam/hospital-middleware/internal/auth"
)

// contextKeyClaims is where verified claims are stashed for downstream
// handlers. Unexported so nothing outside this package can forge them by
// writing to the context directly.
const contextKeyClaims = "auth.claims"

const bearerPrefix = "Bearer "

// RequireAuth verifies the bearer token and puts the claims on the context.
//
// Every failure renders the same 401 body: telling a caller whether the token
// was missing, malformed, expired or signed with the wrong key helps only an
// attacker.
func RequireAuth(tokens *auth.TokenManager) gin.HandlerFunc {
	unauthorized := func(c *gin.Context) {
		err := apierr.Unauthorized("a valid bearer token is required")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{"code": err.Code, "message": err.Message},
		})
	}

	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" || !strings.HasPrefix(header, bearerPrefix) {
			unauthorized(c)
			return
		}

		raw := strings.TrimSpace(strings.TrimPrefix(header, bearerPrefix))
		if raw == "" {
			unauthorized(c)
			return
		}

		claims, err := tokens.Parse(raw)
		if err != nil {
			unauthorized(c)
			return
		}

		c.Set(contextKeyClaims, claims)
		c.Next()
	}
}

// ClaimsFrom returns the verified claims placed by RequireAuth. A false result
// means the route was not protected — a programming error, not a client one.
func ClaimsFrom(c *gin.Context) (*auth.Claims, bool) {
	value, exists := c.Get(contextKeyClaims)
	if !exists {
		return nil, false
	}
	claims, ok := value.(*auth.Claims)
	return claims, ok
}
