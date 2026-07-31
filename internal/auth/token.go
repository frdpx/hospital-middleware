package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/frdpx/hospital-middleware/internal/config"
	"github.com/frdpx/hospital-middleware/internal/models"
)

var ErrInvalidToken = errors.New("auth: invalid token")

type Claims struct {
	jwt.RegisteredClaims
	HospitalID uuid.UUID `json:"hospital_id"`
	Username   string    `json:"username"`
}

func (c *Claims) StaffID() (uuid.UUID, error) {
	return uuid.Parse(c.Subject)
}

type TokenManager struct {
	secret []byte
	issuer string
	ttl    time.Duration

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

func (m *TokenManager) WithClock(now func() time.Time) *TokenManager {
	clone := *m
	clone.now = now
	return &clone
}

func (m *TokenManager) TTL() time.Duration { return m.ttl }

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
