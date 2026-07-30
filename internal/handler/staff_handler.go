package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/bambam/hospital-middleware/internal/service"
)

// StaffHandler serves /staff/create and /staff/login.
type StaffHandler struct {
	staff  *service.StaffService
	logger *slog.Logger
}

func NewStaffHandler(staff *service.StaffService, logger *slog.Logger) *StaffHandler {
	return &StaffHandler{staff: staff, logger: logger}
}

type createStaffRequest struct {
	Username string `json:"username" binding:"required,max=64"`
	Password string `json:"password" binding:"required,min=8,max=72"`
	Hospital string `json:"hospital" binding:"required"`
}

type createStaffResponse struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	Hospital  string    `json:"hospital"`
	CreatedAt time.Time `json:"created_at"`
}

// Create handles POST /staff/create.
//
// The password is never echoed back, and the response carries no password hash
// because models.Staff tags it json:"-".
func (h *StaffHandler) Create(c *gin.Context) {
	var req createStaffRequest
	if err := bindJSON(c, &req); err != nil {
		respondError(c, h.logger, err)
		return
	}

	account, err := h.staff.Create(c.Request.Context(), service.CreateStaffInput{
		Username: req.Username,
		Password: req.Password,
		Hospital: req.Hospital,
	})
	if err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusCreated, createStaffResponse{
		ID:        account.Staff.ID,
		Username:  account.Staff.Username,
		Hospital:  account.Hospital.Code,
		CreatedAt: account.Staff.CreatedAt,
	})
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Hospital string `json:"hospital" binding:"required"`
}

type loginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

// Login handles POST /staff/login and returns a hospital-scoped JWT.
func (h *StaffHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := bindJSON(c, &req); err != nil {
		respondError(c, h.logger, err)
		return
	}

	result, err := h.staff.Login(c.Request.Context(), service.LoginInput{
		Username: req.Username,
		Password: req.Password,
		Hospital: req.Hospital,
	})
	if err != nil {
		respondError(c, h.logger, err)
		return
	}

	c.JSON(http.StatusOK, loginResponse{
		AccessToken: result.AccessToken,
		TokenType:   result.TokenType,
		ExpiresIn:   result.ExpiresIn,
	})
}
