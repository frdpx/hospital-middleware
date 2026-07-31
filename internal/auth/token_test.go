package auth_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/frdpx/hospital-middleware/internal/auth"
	"github.com/frdpx/hospital-middleware/internal/config"
	"github.com/frdpx/hospital-middleware/internal/models"
)

const testSecret = "test-secret-that-is-long-enough-32b"

func testJWTConfig() config.JWTConfig {
	return config.JWTConfig{Secret: testSecret, Issuer: "hospital-middleware", TTL: time.Hour}
}

func testStaff() *models.Staff {
	return &models.Staff{
		ID:         uuid.MustParse("3f1b1e2a-9c3d-4e2a-8b1a-6a1c2d3e4f5a"),
		HospitalID: uuid.MustParse("8a2b1c3d-4e5f-4a7b-8c9d-0e1f2a3b4c5d"),
		Username:   "jsmith",
	}
}

func TestTokenManager_GenerateThenParse(t *testing.T) {
	t.Parallel()

	manager := auth.NewTokenManager(testJWTConfig())
	staff := testStaff()

	token, expiresAt, err := manager.Generate(staff)
	require.NoError(t, err)
	require.NotEmpty(t, token)
	assert.WithinDuration(t, time.Now().Add(time.Hour), expiresAt, 5*time.Second)

	claims, err := manager.Parse(token)
	require.NoError(t, err)

	assert.Equal(t, staff.HospitalID, claims.HospitalID, "hospital scope must survive the round trip")
	assert.Equal(t, "jsmith", claims.Username)

	staffID, err := claims.StaffID()
	require.NoError(t, err)
	assert.Equal(t, staff.ID, staffID)
}

func TestTokenManager_Parse_Rejections(t *testing.T) {
	t.Parallel()

	cfg := testJWTConfig()
	manager := auth.NewTokenManager(cfg)
	staff := testStaff()

	pastManager := manager.WithClock(func() time.Time { return time.Now().Add(-2 * time.Hour) })
	expiredToken, _, err := pastManager.Generate(staff)
	require.NoError(t, err)

	forgedCfg := cfg
	forgedCfg.Secret = "a-completely-different-secret-key-32"
	forgedToken, _, err := auth.NewTokenManager(forgedCfg).Generate(staff)
	require.NoError(t, err)

	otherIssuerCfg := cfg
	otherIssuerCfg.Issuer = "some-other-service"
	otherIssuerToken, _, err := auth.NewTokenManager(otherIssuerCfg).Generate(staff)
	require.NoError(t, err)

	tests := []struct {
		name  string
		token string
	}{
		{name: "empty string", token: ""},
		{name: "not a jwt at all", token: "this.is.garbage"},
		{name: "expired token", token: expiredToken},
		{name: "signed with the wrong secret", token: forgedToken},
		{name: "issued by a different service", token: otherIssuerToken},
		{name: "unsigned alg=none token", token: unsignedToken(t, staff)},
		{name: "token carrying no hospital scope", token: tokenWithoutHospital(t, staff)},
		{name: "token whose subject is not a staff id", token: tokenWithSubject(t, "not-a-uuid")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			claims, err := manager.Parse(tc.token)

			assert.Nil(t, claims)
			require.Error(t, err)
			assert.ErrorIs(t, err, auth.ErrInvalidToken)
		})
	}
}

func unsignedToken(t *testing.T, staff *models.Staff) string {
	t.Helper()

	claims := auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   staff.ID.String(),
			Issuer:    "hospital-middleware",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		HospitalID: staff.HospitalID,
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	return token
}

func tokenWithoutHospital(t *testing.T, staff *models.Staff) string {
	t.Helper()

	claims := auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   staff.ID.String(),
			Issuer:    "hospital-middleware",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	require.NoError(t, err)
	return token
}

func tokenWithSubject(t *testing.T, subject string) string {
	t.Helper()

	claims := auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			Issuer:    "hospital-middleware",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		HospitalID: uuid.New(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	require.NoError(t, err)
	return token
}

func TestTokenManager_ScopesTokensPerHospital(t *testing.T) {
	t.Parallel()

	manager := auth.NewTokenManager(testJWTConfig())

	hospitalA := uuid.New()
	hospitalB := uuid.New()

	tokenA, _, err := manager.Generate(&models.Staff{ID: uuid.New(), HospitalID: hospitalA, Username: "jsmith"})
	require.NoError(t, err)
	tokenB, _, err := manager.Generate(&models.Staff{ID: uuid.New(), HospitalID: hospitalB, Username: "jsmith"})
	require.NoError(t, err)

	claimsA, err := manager.Parse(tokenA)
	require.NoError(t, err)
	claimsB, err := manager.Parse(tokenB)
	require.NoError(t, err)

	assert.Equal(t, hospitalA, claimsA.HospitalID)
	assert.Equal(t, hospitalB, claimsB.HospitalID)
	assert.NotEqual(t, claimsA.HospitalID, claimsB.HospitalID)
}
