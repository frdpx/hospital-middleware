package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/frdpx/hospital-middleware/internal/auth"
	"github.com/frdpx/hospital-middleware/internal/middleware"
)

type RouterDeps struct {
	Staff    *StaffHandler
	Patients *PatientHandler
	Tokens   *auth.TokenManager
	Logger   *slog.Logger

	Ping func(ctx context.Context) error

	Debug bool
}

func NewRouter(deps RouterDeps) *gin.Engine {
	useJSONFieldNames()

	if deps.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	router.HandleMethodNotAllowed = true
	router.Use(
		middleware.RequestID(),
		middleware.Recovery(deps.Logger),
		middleware.AccessLog(deps.Logger),
	)

	_ = router.SetTrustedProxies(nil)

	router.GET("/healthz", healthz)
	router.GET("/readyz", readyz(deps.Ping, deps.Logger))

	staff := router.Group("/staff")
	{
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

func healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

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
