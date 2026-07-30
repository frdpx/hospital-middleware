package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bambam/hospital-middleware/internal/auth"
	"github.com/bambam/hospital-middleware/internal/middleware"
)

// RouterDeps is everything the HTTP layer needs. Passing one struct keeps
// wiring in main.go readable and makes the router trivially constructible in
// tests with fakes.
type RouterDeps struct {
	Staff    *StaffHandler
	Patients *PatientHandler
	Tokens   *auth.TokenManager
	Logger   *slog.Logger
	// Ping reports whether dependencies (the database) are reachable. Used by
	// the readiness probe that docker-compose and nginx rely on.
	Ping func(ctx context.Context) error
	// Debug switches gin into development mode.
	Debug bool
}

// NewRouter builds the Gin engine with all routes and middleware.
func NewRouter(deps RouterDeps) *gin.Engine {
	useJSONFieldNames()

	if deps.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	// Without this, gin answers a wrong-method request with 404, which reads
	// as "no such endpoint" and sends clients hunting for a typo in the path.
	router.HandleMethodNotAllowed = true
	router.Use(
		middleware.RequestID(),
		middleware.Recovery(deps.Logger),
		middleware.AccessLog(deps.Logger),
	)

	// Trust no proxy headers by default. Nginx sits in front of this service,
	// but blanket-trusting X-Forwarded-For would let a client spoof its own
	// address in our logs.
	_ = router.SetTrustedProxies(nil)

	router.GET("/healthz", healthz)
	router.GET("/readyz", readyz(deps.Ping, deps.Logger))

	staff := router.Group("/staff")
	{
		// Left unauthenticated per the assignment's spec. See docs/adr/0003.
		staff.POST("/create", deps.Staff.Create)
		staff.POST("/login", deps.Staff.Login)
	}

	patient := router.Group("/patient")
	patient.Use(middleware.RequireAuth(deps.Tokens))
	{
		patient.POST("/search", deps.Patients.Search)
	}

	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, errorEnvelope{
			Error: errorDetail{Code: "ROUTE_NOT_FOUND", Message: "no such endpoint"},
		})
	})
	router.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, errorEnvelope{
			Error: errorDetail{Code: "METHOD_NOT_ALLOWED", Message: "method not allowed for this endpoint"},
		})
	})

	return router
}

// healthz is a liveness probe: it answers as long as the process is running.
func healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// readyz is a readiness probe: it fails while the database is unreachable, so
// a restarting stack does not receive traffic before it can serve it.
func readyz(ping func(ctx context.Context) error, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if ping == nil {
			c.JSON(http.StatusOK, gin.H{"status": "ready"})
			return
		}
		if err := ping(c.Request.Context()); err != nil {
			logger.WarnContext(c.Request.Context(), "readiness check failed", "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "dependency": "database"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}
