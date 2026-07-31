package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/frdpx/hospital-middleware/internal/apierr"
	"github.com/frdpx/hospital-middleware/internal/auth"
	"github.com/frdpx/hospital-middleware/internal/config"
	"github.com/frdpx/hospital-middleware/internal/models"
	"github.com/frdpx/hospital-middleware/internal/service"
	"github.com/frdpx/hospital-middleware/internal/testutil"
)

var (
	hospitalAID = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	hospitalBID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

func testHospitals() []models.Hospital {
	return []models.Hospital{
		{ID: hospitalAID, Code: "hospital-a", Name: "Hospital A", HISAdapterType: models.HISAdapterHospitalA, HISBaseURL: testutil.Ptr("https://hospital-a.api.co.th")},
		{ID: hospitalBID, Code: "hospital-b", Name: "Hospital B", HISAdapterType: models.HISAdapterHospitalA, HISBaseURL: testutil.Ptr("https://hospital-b.api.co.th")},
	}
}

func testTokenManager() *auth.TokenManager {
	return auth.NewTokenManager(config.JWTConfig{
		Secret: "test-secret-that-is-long-enough-32b",
		Issuer: "hospital-middleware",
		TTL:    time.Hour,
	})
}

func newStaffService(t *testing.T, staff ...models.Staff) (*service.StaffService, *testutil.StaffRepo) {
	t.Helper()

	staffRepo := testutil.NewStaffRepo(staff...)
	svc := service.NewStaffService(
		testutil.NewHospitalRepo(testHospitals()...),
		staffRepo,
		testTokenManager(),
	).WithBcryptCost(bcrypt.MinCost)
	return svc, staffRepo
}

func hashedStaff(t *testing.T, hospitalID uuid.UUID, username, password string) models.Staff {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	return models.Staff{
		ID:           uuid.New(),
		HospitalID:   hospitalID,
		Username:     username,
		PasswordHash: string(hash),
	}
}

func TestStaffService_Create_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        service.CreateStaffInput
		wantUsername string
		wantHospital uuid.UUID
	}{
		{
			name:         "creates an account against the hospital slug",
			input:        service.CreateStaffInput{Username: "jsmith", Password: "P@ssw0rd123", Hospital: "hospital-a"},
			wantUsername: "jsmith",
			wantHospital: hospitalAID,
		},
		{
			name:         "hospital may also be given by display name",
			input:        service.CreateStaffInput{Username: "jsmith", Password: "P@ssw0rd123", Hospital: "Hospital A"},
			wantUsername: "jsmith",
			wantHospital: hospitalAID,
		},
		{
			name:         "surrounding whitespace in the username is trimmed",
			input:        service.CreateStaffInput{Username: "  jsmith  ", Password: "P@ssw0rd123", Hospital: "hospital-a"},
			wantUsername: "jsmith",
			wantHospital: hospitalAID,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, _ := newStaffService(t)

			account, err := svc.Create(context.Background(), tc.input)

			require.NoError(t, err)
			require.NotNil(t, account)
			assert.Equal(t, tc.wantUsername, account.Staff.Username)
			assert.Equal(t, tc.wantHospital, account.Staff.HospitalID)
			assert.NotEqual(t, uuid.Nil, account.Staff.ID)
		})
	}
}

func TestStaffService_Create_StoresBcryptHashNotPlaintext(t *testing.T) {
	t.Parallel()

	const password = "P@ssw0rd123"
	svc, repo := newStaffService(t)

	account, err := svc.Create(context.Background(), service.CreateStaffInput{
		Username: "jsmith", Password: password, Hospital: "hospital-a",
	})
	require.NoError(t, err)

	assert.NotEqual(t, password, account.Staff.PasswordHash)
	assert.True(t, strings.HasPrefix(account.Staff.PasswordHash, "$2"), "password must be bcrypt-hashed at rest")
	assert.NoError(t,
		bcrypt.CompareHashAndPassword([]byte(account.Staff.PasswordHash), []byte(password)),
		"the stored hash must still verify the original password")
	assert.Equal(t, 1, repo.Count())
}

func TestStaffService_Create_Rejections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    service.CreateStaffInput
		wantCode string
		wantMsg  string
	}{
		{
			name:     "missing username",
			input:    service.CreateStaffInput{Password: "P@ssw0rd123", Hospital: "hospital-a"},
			wantCode: apierr.CodeValidation,
			wantMsg:  "username is required",
		},
		{
			name:     "username of only whitespace is empty",
			input:    service.CreateStaffInput{Username: "   ", Password: "P@ssw0rd123", Hospital: "hospital-a"},
			wantCode: apierr.CodeValidation,
		},
		{
			name:     "missing password",
			input:    service.CreateStaffInput{Username: "jsmith", Hospital: "hospital-a"},
			wantCode: apierr.CodeValidation,
			wantMsg:  "password is required",
		},
		{
			name:     "password shorter than the minimum",
			input:    service.CreateStaffInput{Username: "jsmith", Password: "short", Hospital: "hospital-a"},
			wantCode: apierr.CodeValidation,
			wantMsg:  "at least 8",
		},
		{
			name:     "password longer than bcrypt's 72-byte limit",
			input:    service.CreateStaffInput{Username: "jsmith", Password: strings.Repeat("a", 73), Hospital: "hospital-a"},
			wantCode: apierr.CodeValidation,
			wantMsg:  "at most 72",
		},
		{
			name:     "missing hospital",
			input:    service.CreateStaffInput{Username: "jsmith", Password: "P@ssw0rd123"},
			wantCode: apierr.CodeValidation,
			wantMsg:  "hospital is required",
		},
		{
			name:     "hospital that does not exist",
			input:    service.CreateStaffInput{Username: "jsmith", Password: "P@ssw0rd123", Hospital: "hospital-zzz"},
			wantCode: apierr.CodeHospitalNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, repo := newStaffService(t)

			account, err := svc.Create(context.Background(), tc.input)

			assert.Nil(t, account)
			require.Error(t, err)
			assert.Equal(t, tc.wantCode, apierr.From(err).Code)
			if tc.wantMsg != "" {
				assert.Contains(t, apierr.From(err).Message, tc.wantMsg)
			}
			assert.Equal(t, 0, repo.Count(), "a rejected request must not create an account")
		})
	}
}

func TestStaffService_Create_DuplicateUsernameWithinHospital(t *testing.T) {
	t.Parallel()

	svc, repo := newStaffService(t)
	input := service.CreateStaffInput{Username: "jsmith", Password: "P@ssw0rd123", Hospital: "hospital-a"}

	_, err := svc.Create(context.Background(), input)
	require.NoError(t, err)

	account, err := svc.Create(context.Background(), input)

	assert.Nil(t, account)
	require.Error(t, err)
	assert.Equal(t, apierr.CodeUsernameTaken, apierr.From(err).Code)
	assert.Equal(t, 1, repo.Count())
}

func TestStaffService_Create_SameUsernameAtDifferentHospitalsIsAllowed(t *testing.T) {
	t.Parallel()

	svc, repo := newStaffService(t)

	first, err := svc.Create(context.Background(), service.CreateStaffInput{
		Username: "jsmith", Password: "P@ssw0rd123", Hospital: "hospital-a",
	})
	require.NoError(t, err)

	second, err := svc.Create(context.Background(), service.CreateStaffInput{
		Username: "jsmith", Password: "different-password", Hospital: "hospital-b",
	})
	require.NoError(t, err)

	assert.Equal(t, "jsmith", first.Staff.Username)
	assert.Equal(t, "jsmith", second.Staff.Username)
	assert.NotEqual(t, first.Staff.HospitalID, second.Staff.HospitalID)
	assert.Equal(t, 2, repo.Count())
}

func TestStaffService_Login_Success(t *testing.T) {
	t.Parallel()

	const password = "P@ssw0rd123"
	existing := hashedStaff(t, hospitalAID, "jsmith", password)
	svc, _ := newStaffService(t, existing)

	result, err := svc.Login(context.Background(), service.LoginInput{
		Username: "jsmith", Password: password, Hospital: "hospital-a",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.AccessToken)
	assert.Equal(t, "Bearer", result.TokenType)
	assert.Equal(t, int64(3600), result.ExpiresIn)

	claims, err := testTokenManager().Parse(result.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, hospitalAID, claims.HospitalID, "the token must be scoped to the staff member's hospital")
	assert.Equal(t, existing.ID.String(), claims.Subject)
}

func TestStaffService_Login_Rejections(t *testing.T) {
	t.Parallel()

	const password = "P@ssw0rd123"

	staffAtA := hashedStaff(t, hospitalAID, "jsmith", password)
	staffAtB := hashedStaff(t, hospitalBID, "jsmith", "hospital-b-password")

	tests := []struct {
		name     string
		input    service.LoginInput
		wantCode string
	}{
		{
			name:     "wrong password",
			input:    service.LoginInput{Username: "jsmith", Password: "wrong-password", Hospital: "hospital-a"},
			wantCode: apierr.CodeInvalidCredentials,
		},
		{
			name:     "username that does not exist",
			input:    service.LoginInput{Username: "nobody", Password: password, Hospital: "hospital-a"},
			wantCode: apierr.CodeInvalidCredentials,
		},
		{
			name:     "hospital that does not exist looks exactly like bad credentials",
			input:    service.LoginInput{Username: "jsmith", Password: password, Hospital: "hospital-zzz"},
			wantCode: apierr.CodeInvalidCredentials,
		},
		{
			name:     "right username and password but the wrong hospital",
			input:    service.LoginInput{Username: "jsmith", Password: password, Hospital: "hospital-b"},
			wantCode: apierr.CodeInvalidCredentials,
		},
		{
			name:     "missing username",
			input:    service.LoginInput{Password: password, Hospital: "hospital-a"},
			wantCode: apierr.CodeValidation,
		},
		{
			name:     "missing password",
			input:    service.LoginInput{Username: "jsmith", Hospital: "hospital-a"},
			wantCode: apierr.CodeValidation,
		},
		{
			name:     "missing hospital",
			input:    service.LoginInput{Username: "jsmith", Password: password},
			wantCode: apierr.CodeValidation,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, _ := newStaffService(t, staffAtA, staffAtB)

			result, err := svc.Login(context.Background(), tc.input)

			assert.Nil(t, result)
			require.Error(t, err)
			assert.Equal(t, tc.wantCode, apierr.From(err).Code)
			assert.NotContains(t, apierr.From(err).Message, "hospital does not exist",
				"the error must not reveal which part of the credentials was wrong")
		})
	}
}

func TestStaffService_Login_SameUsernameDifferentHospitals(t *testing.T) {
	t.Parallel()

	staffAtA := hashedStaff(t, hospitalAID, "jsmith", "password-at-a")
	staffAtB := hashedStaff(t, hospitalBID, "jsmith", "password-at-b")
	svc, _ := newStaffService(t, staffAtA, staffAtB)

	resultA, err := svc.Login(context.Background(), service.LoginInput{
		Username: "jsmith", Password: "password-at-a", Hospital: "hospital-a",
	})
	require.NoError(t, err)

	resultB, err := svc.Login(context.Background(), service.LoginInput{
		Username: "jsmith", Password: "password-at-b", Hospital: "hospital-b",
	})
	require.NoError(t, err)

	claimsA, err := testTokenManager().Parse(resultA.AccessToken)
	require.NoError(t, err)
	claimsB, err := testTokenManager().Parse(resultB.AccessToken)
	require.NoError(t, err)

	assert.Equal(t, hospitalAID, claimsA.HospitalID)
	assert.Equal(t, hospitalBID, claimsB.HospitalID)

	_, err = svc.Login(context.Background(), service.LoginInput{
		Username: "jsmith", Password: "password-at-b", Hospital: "hospital-a",
	})
	assert.Equal(t, apierr.CodeInvalidCredentials, apierr.From(err).Code)
}

func TestStaffService_Login_RepositoryFailureIsNotLeakedToTheClient(t *testing.T) {
	t.Parallel()

	staffRepo := testutil.NewStaffRepo()
	staffRepo.FindErr = apierr.Internal().WithCause(errors.New("connection reset by peer"))

	svc := service.NewStaffService(
		testutil.NewHospitalRepo(testHospitals()...),
		staffRepo,
		testTokenManager(),
	).WithBcryptCost(bcrypt.MinCost)

	result, err := svc.Login(context.Background(), service.LoginInput{
		Username: "jsmith", Password: "P@ssw0rd123", Hospital: "hospital-a",
	})

	assert.Nil(t, result)
	require.Error(t, err)
	apiErr := apierr.From(err)
	assert.Equal(t, apierr.CodeInternal, apiErr.Code)
	assert.NotContains(t, apiErr.Message, "connection reset", "internal details must stay in the logs")
}
