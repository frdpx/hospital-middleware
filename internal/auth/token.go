// Package auth issues and verifies the JWTs that carry a staff member's
// identity and — critically — their hospital scope.
//
// hospital_id lives in the signed token rather than in the request body so a
// client cannot ask for another hospital's data: the scope is fixed at login
// and cryptographically sealed.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/bambam/hospital-middleware/internal/config"
	"github.com/bambam/hospital-middleware/internal/models"
)

// ErrInvalidToken covers every reason a token was rejected. The reason is
// deliberately not exposed to clients — "expired" vs "bad signature" is useful
// to an attacker and to nobody else.
var ErrInvalidToken = errors.New("auth: invalid token")

// Claims is the JWT payload. HospitalID is the authorization scope every
// patient query is filtered by.
type Claims struct {
	jwt.RegisteredClaims
	HospitalID uuid.UUID `json:"hospital_id"`
	Username   string    `json:"username"`
}

// StaffID returns the staff member's id from the standard `sub` claim.
func (c *Claims) StaffID() (uuid.UUID, error) {
	return uuid.Parse(c.Subject)
}

// TokenManager signs and verifies access tokens.
type TokenManager struct {
	secret []byte
	issuer string
	ttl    time.Duration
	// now is injectable so tests can produce already-expired tokens without
	// sleeping.
	now func() time.Time
}

func NewTokenManager(cfg config.JWTConfig) *TokenManager {
	return &TokenManager{
		secret: []byte(cfg.Secret),
		issuer: cfg.Issuer,
		ttl:    cfg.TTL,
		now:    time.Now,
	}
}

// WithClock overrides the clock. Test-only helper.
func (m *TokenManager) WithClock(now func() time.Time) *TokenManager {
	clone := *m
	clone.now = now
	return &clone
}

// TTL exposes the configured lifetime so handlers can report expires_in.
func (m *TokenManager) TTL() time.Duration { return m.ttl }

// Generate signs an access token for a staff member.
func (m *TokenManager) Generate(staff *models.Staff) (string, time.Time, error) {
	issuedAt := m.now()
	expiresAt := issuedAt.Add(m.ttl)

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   staff.ID.String(),
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			NotBefore: jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			ID:        uuid.NewString(),
		},
		HospitalID: staff.HospitalID,
		Username:   staff.Username,
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign token: %w", err)
	}
	return signed, expiresAt, nil
}

// Parse verifies a token and returns its claims.
//
// The allowed signing method is pinned to HMAC: without that check an attacker
// could present an "alg: none" token, or a token signed with our public key if
// we ever moved to RS256, and have it accepted.
func (m *TokenManager) Parse(tokenString string) (*Claims, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(m.now),
	)

	claims := &Claims{}
	token, err := parser.ParseWithClaims(tokenString, claims, func(*jwt.Token) (any, error) {
		return m.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if !token.Valid {
		return nil, ErrInvalidToken
	}
	if claims.HospitalID == uuid.Nil {
		return nil, fmt.Errorf("%w: token carries no hospital scope", ErrInvalidToken)
	}
	if _, err := claims.StaffID(); err != nil {
		return nil, fmt.Errorf("%w: subject is not a staff id", ErrInvalidToken)
	}
	return claims, nil
}
