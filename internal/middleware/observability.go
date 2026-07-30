package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestIDHeader is echoed on every response so a client can quote it in a
// bug report and it can be grepped straight out of the logs.
const RequestIDHeader = "X-Request-ID"

// RequestID assigns each request a correlation id, reusing the caller's if it
// supplied one (so an id survives the hop through nginx).
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		c.Set("request_id", id)
		c.Header(RequestIDHeader, id)
		c.Next()
	}
}

// AccessLog emits one structured line per request.
//
// It deliberately logs no request body and no query string: those carry
// national ids and patient names, which do not belong in application logs.
func AccessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		attrs := []any{
			"method", c.Request.Method,
			"path", c.FullPath(),
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", c.GetString("request_id"),
		}
		if claims, ok := ClaimsFrom(c); ok {
			attrs = append(attrs, "staff_id", claims.Subject, "hospital_id", claims.HospitalID)
		}

		if c.Writer.Status() >= 500 {
			logger.Error("request", attrs...)
			return
		}
		logger.Info("request", attrs...)
	}
}

// Recovery converts a panic into a 500 with our standard error envelope,
// instead of gin's default HTML-ish output, and logs the stack.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, recovered any) {
		logger.Error("panic recovered",
			"panic", recovered,
			"path", c.FullPath(),
			"request_id", c.GetString("request_id"),
		)
		c.AbortWithStatusJSON(500, gin.H{
			"error": gin.H{"code": "INTERNAL_ERROR", "message": "internal server error"},
		})
	})
}
