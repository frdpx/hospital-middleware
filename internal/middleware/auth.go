package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/bambam/hospital-middleware/internal/apierr"
	"github.com/bambam/hospital-middleware/internal/auth"
)

const contextKeyClaims = "auth.claims"

const bearerPrefix = "Bearer "

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

func ClaimsFrom(c *gin.Context) (*auth.Claims, bool) {
	value, exists := c.Get(contextKeyClaims)
	if !exists {
		return nil, false
	}
	claims, ok := value.(*auth.Claims)
	return claims, ok
}
