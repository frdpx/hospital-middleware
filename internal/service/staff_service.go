// Package service holds the business rules. It depends on narrow interfaces
// (declared here, at the point of use) rather than on concrete repositories,
// which is what makes every rule below unit-testable without a database.
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
	// bcrypt silently truncates at 72 bytes; rejecting longer input is clearer
	// than accepting a password whose tail is ignored.
	maxPasswordLength = 72
	maxUsernameLength = 64
)

// HospitalRepository is the slice of hospital storage the services need.
type HospitalRepository interface {
	FindByCodeOrName(ctx context.Context, identifier string) (*models.Hospital, error)
	FindByID(ctx context.Context, id uuid.UUID) (*models.Hospital, error)
}

// StaffRepository is the slice of staff storage the services need.
type StaffRepository interface {
	Create(ctx context.Context, staff *models.Staff) (*models.Staff, error)
	// FindByHospitalAndUsername returns (nil, nil) when no such staff exists.
	FindByHospitalAndUsername(ctx context.Context, hospitalID uuid.UUID, username string) (*models.Staff, error)
}

// CreateStaffInput is the validated shape of POST /staff/create.
type CreateStaffInput struct {
	Username string
	Password string
	Hospital string
}

// LoginInput is the validated shape of POST /staff/login.
type LoginInput struct {
	Username string
	Password string
	Hospital string
}

// StaffAccount pairs a staff row with the hospital it belongs to, so handlers
// can echo the hospital's code back without a second lookup.
type StaffAccount struct {
	Staff    *models.Staff
	Hospital *models.Hospital
}

// LoginResult is what a successful login yields.
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
	// bcryptCost is configurable so tests can run at MinCost; production uses
	// bcrypt.DefaultCost, which is deliberately slow.
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

// WithBcryptCost returns a copy using the given cost. Test-only helper: the
// default cost makes a table of login tests take tens of seconds.
func (s *StaffService) WithBcryptCost(cost int) *StaffService {
	clone := *s
	clone.bcryptCost = cost
	return &clone
}

// Create registers a new staff account at an existing hospital.
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

	// The unique index on (hospital_id, lower(username)) is the authority on
	// duplicates — a read-then-write check here would still race.
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

// Login verifies credentials and issues a hospital-scoped access token.
func (s *StaffService) Login(ctx context.Context, input LoginInput) (*LoginResult, error) {
	username := strings.TrimSpace(input.Username)
	if username == "" || input.Password == "" || strings.TrimSpace(input.Hospital) == "" {
		return nil, apierr.Validation("username, password and hospital are required")
	}

	// A hospital that does not exist must look exactly like a wrong password,
	// otherwise the endpoint becomes a hospital/username enumeration oracle.
	hospital, err := s.hospitals.FindByCodeOrName(ctx, strings.TrimSpace(input.Hospital))
	if err != nil {
		if apiErr := apierr.From(err); apiErr.Code == apierr.CodeHospitalNotFound {
			comparePasswordWithDummyHash(input.Password)
			return nil, apierr.InvalidCredentials()
		}
		return nil, err
	}

	// Looking up by (hospital_id, username) rather than username alone is what
	// keeps two hospitals' identically-named staff separate.
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

// dummyHash is a valid bcrypt hash of a value nobody knows. Comparing against
// it on the "no such user" path keeps the response time of a wrong username
// close to that of a wrong password, denying an attacker a timing oracle.
var dummyHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

func comparePasswordWithDummyHash(password string) {
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
}
