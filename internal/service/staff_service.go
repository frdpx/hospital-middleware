package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/bambam/hospital-middleware/internal/apierr"
	"github.com/bambam/hospital-middleware/internal/auth"
	"github.com/bambam/hospital-middleware/internal/models"
)

const (
	minPasswordLength = 8

	maxPasswordLength = 72
	maxUsernameLength = 64
)

type HospitalRepository interface {
	FindByCodeOrName(ctx context.Context, identifier string) (*models.Hospital, error)
	FindByID(ctx context.Context, id uuid.UUID) (*models.Hospital, error)
}

type StaffRepository interface {
	Create(ctx context.Context, staff *models.Staff) (*models.Staff, error)

	FindByHospitalAndUsername(ctx context.Context, hospitalID uuid.UUID, username string) (*models.Staff, error)
}

type CreateStaffInput struct {
	Username string
	Password string
	Hospital string
}

type LoginInput struct {
	Username string
	Password string
	Hospital string
}

type StaffAccount struct {
	Staff    *models.Staff
	Hospital *models.Hospital
}

type LoginResult struct {
	AccessToken string
	TokenType   string
	ExpiresIn   int64
	ExpiresAt   time.Time
	Staff       *models.Staff
}

type StaffService struct {
	hospitals HospitalRepository
	staff     StaffRepository
	tokens    *auth.TokenManager

	bcryptCost int
}

func NewStaffService(hospitals HospitalRepository, staff StaffRepository, tokens *auth.TokenManager) *StaffService {
	return &StaffService{
		hospitals:  hospitals,
		staff:      staff,
		tokens:     tokens,
		bcryptCost: bcrypt.DefaultCost,
	}
}

func (s *StaffService) WithBcryptCost(cost int) *StaffService {
	clone := *s
	clone.bcryptCost = cost
	return &clone
}

func (s *StaffService) Create(ctx context.Context, input CreateStaffInput) (*StaffAccount, error) {
	username := strings.TrimSpace(input.Username)
	if err := validateCredentials(username, input.Password, input.Hospital); err != nil {
		return nil, err
	}

	hospital, err := s.hospitals.FindByCodeOrName(ctx, strings.TrimSpace(input.Hospital))
	if err != nil {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), s.bcryptCost)
	if err != nil {
		return nil, apierr.Internal().WithCause(err)
	}

	created, err := s.staff.Create(ctx, &models.Staff{
		HospitalID:   hospital.ID,
		Username:     username,
		PasswordHash: string(hash),
	})
	if err != nil {
		return nil, err
	}
	return &StaffAccount{Staff: created, Hospital: hospital}, nil
}

func (s *StaffService) Login(ctx context.Context, input LoginInput) (*LoginResult, error) {
	username := strings.TrimSpace(input.Username)
	if username == "" || input.Password == "" || strings.TrimSpace(input.Hospital) == "" {
		return nil, apierr.Validation("username, password and hospital are required")
	}

	hospital, err := s.hospitals.FindByCodeOrName(ctx, strings.TrimSpace(input.Hospital))
	if err != nil {
		if apiErr := apierr.From(err); apiErr.Code == apierr.CodeHospitalNotFound {
			comparePasswordWithDummyHash(input.Password)
			return nil, apierr.InvalidCredentials()
		}
		return nil, err
	}

	staff, err := s.staff.FindByHospitalAndUsername(ctx, hospital.ID, username)
	if err != nil {
		return nil, err
	}
	if staff == nil {
		comparePasswordWithDummyHash(input.Password)
		return nil, apierr.InvalidCredentials()
	}

	if err := bcrypt.CompareHashAndPassword([]byte(staff.PasswordHash), []byte(input.Password)); err != nil {
		return nil, apierr.InvalidCredentials()
	}

	token, expiresAt, err := s.tokens.Generate(staff)
	if err != nil {
		return nil, apierr.Internal().WithCause(err)
	}

	return &LoginResult{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(s.tokens.TTL().Seconds()),
		ExpiresAt:   expiresAt,
		Staff:       staff,
	}, nil
}

func validateCredentials(username, password, hospital string) error {
	switch {
	case username == "":
		return apierr.Validation("username is required")
	case len(username) > maxUsernameLength:
		return apierr.Validation("username must be at most 64 characters")
	case password == "":
		return apierr.Validation("password is required")
	case len(password) < minPasswordLength:
		return apierr.Validation("password must be at least 8 characters")
	case len(password) > maxPasswordLength:
		return apierr.Validation("password must be at most 72 characters")
	case strings.TrimSpace(hospital) == "":
		return apierr.Validation("hospital is required")
	}
	return nil
}

var dummyHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

func comparePasswordWithDummyHash(password string) {
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
}
