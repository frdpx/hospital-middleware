package handler_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bambam/hospital-middleware/internal/apierr"
)

func TestStaffCreate_Success(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	recorder := server.do(t, http.MethodPost, "/staff/create", map[string]string{
		"username": "jsmith",
		"password": "P@ssw0rd123",
		"hospital": "hospital-a",
	}, "")

	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())

	body := decode[map[string]any](t, recorder)
	assert.Equal(t, "jsmith", body["username"])
	assert.Equal(t, "hospital-a", body["hospital"])
	assert.NotEmpty(t, body["id"])
	assert.NotEmpty(t, body["created_at"])

	assert.NotContains(t, recorder.Body.String(), "P@ssw0rd123")
	assert.NotContains(t, strings.ToLower(recorder.Body.String()), "password")
}

func TestStaffCreate_Rejections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       any
		wantStatus int
		wantCode   string
	}{
		{
			name:       "missing username",
			body:       map[string]string{"password": "P@ssw0rd123", "hospital": "hospital-a"},
			wantStatus: http.StatusBadRequest,
			wantCode:   apierr.CodeValidation,
		},
		{
			name:       "missing password",
			body:       map[string]string{"username": "jsmith", "hospital": "hospital-a"},
			wantStatus: http.StatusBadRequest,
			wantCode:   apierr.CodeValidation,
		},
		{
			name:       "password shorter than 8 characters",
			body:       map[string]string{"username": "jsmith", "password": "short", "hospital": "hospital-a"},
			wantStatus: http.StatusBadRequest,
			wantCode:   apierr.CodeValidation,
		},
		{
			name:       "missing hospital",
			body:       map[string]string{"username": "jsmith", "password": "P@ssw0rd123"},
			wantStatus: http.StatusBadRequest,
			wantCode:   apierr.CodeValidation,
		},
		{
			name:       "malformed JSON",
			body:       `{"username": "jsmith", `,
			wantStatus: http.StatusBadRequest,
			wantCode:   apierr.CodeValidation,
		},
		{
			name:       "wrong field types",
			body:       `{"username": 42, "password": true, "hospital": []}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   apierr.CodeValidation,
		},
		{
			name:       "hospital that does not exist",
			body:       map[string]string{"username": "jsmith", "password": "P@ssw0rd123", "hospital": "hospital-zzz"},
			wantStatus: http.StatusNotFound,
			wantCode:   apierr.CodeHospitalNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := newTestServer(t)

			recorder := server.do(t, http.MethodPost, "/staff/create", tc.body, "")

			assert.Equal(t, tc.wantStatus, recorder.Code, recorder.Body.String())
			assert.Equal(t, tc.wantCode, errorCode(t, recorder))
			assert.Equal(t, 0, server.staff.Count())
		})
	}
}

func TestStaffCreate_ValidationMessagesUseJSONFieldNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    map[string]string
		wantMsg string
	}{
		{
			name:    "missing field names the json key",
			body:    map[string]string{"password": "P@ssw0rd123", "hospital": "hospital-a"},
			wantMsg: "username is required",
		},
		{
			name:    "min-length failure names the json key",
			body:    map[string]string{"username": "jsmith", "password": "short", "hospital": "hospital-a"},
			wantMsg: "password must be at least 8 characters",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := newTestServer(t)

			recorder := server.do(t, http.MethodPost, "/staff/create", tc.body, "")

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			body := decode[map[string]map[string]string](t, recorder)
			assert.Equal(t, tc.wantMsg, body["error"]["message"])
		})
	}
}

func TestStaffCreate_DuplicateUsernameInSameHospital(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	payload := map[string]string{"username": "jsmith", "password": "P@ssw0rd123", "hospital": "hospital-a"}

	first := server.do(t, http.MethodPost, "/staff/create", payload, "")
	require.Equal(t, http.StatusCreated, first.Code)

	second := server.do(t, http.MethodPost, "/staff/create", payload, "")

	assert.Equal(t, http.StatusConflict, second.Code)
	assert.Equal(t, apierr.CodeUsernameTaken, errorCode(t, second))
	assert.Equal(t, 1, server.staff.Count())
}

func TestStaffCreate_SameUsernameAtAnotherHospitalIsAllowed(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	atA := server.do(t, http.MethodPost, "/staff/create", map[string]string{
		"username": "jsmith", "password": "P@ssw0rd123", "hospital": "hospital-a",
	}, "")
	atB := server.do(t, http.MethodPost, "/staff/create", map[string]string{
		"username": "jsmith", "password": "P@ssw0rd456", "hospital": "hospital-b",
	}, "")

	require.Equal(t, http.StatusCreated, atA.Code)
	require.Equal(t, http.StatusCreated, atB.Code, atB.Body.String())
	assert.Equal(t, 2, server.staff.Count())
}

func TestStaffLogin_Success(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	staff := server.seedStaff(t, hospitalAID, "jsmith", "P@ssw0rd123")

	recorder := server.do(t, http.MethodPost, "/staff/login", map[string]string{
		"username": "jsmith", "password": "P@ssw0rd123", "hospital": "hospital-a",
	}, "")

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	body := decode[struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}](t, recorder)

	assert.Equal(t, "Bearer", body.TokenType)
	assert.Equal(t, int64(3600), body.ExpiresIn)
	require.NotEmpty(t, body.AccessToken)

	claims, err := server.tokens.Parse(body.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, hospitalAID, claims.HospitalID)
	assert.Equal(t, staff.ID.String(), claims.Subject)
}

func TestStaffLogin_Rejections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       any
		wantStatus int
		wantCode   string
	}{
		{
			name:       "wrong password",
			body:       map[string]string{"username": "jsmith", "password": "wrong-password", "hospital": "hospital-a"},
			wantStatus: http.StatusUnauthorized,
			wantCode:   apierr.CodeInvalidCredentials,
		},
		{
			name:       "unknown username",
			body:       map[string]string{"username": "nobody", "password": "P@ssw0rd123", "hospital": "hospital-a"},
			wantStatus: http.StatusUnauthorized,
			wantCode:   apierr.CodeInvalidCredentials,
		},
		{
			name:       "correct credentials but the wrong hospital",
			body:       map[string]string{"username": "jsmith", "password": "P@ssw0rd123", "hospital": "hospital-b"},
			wantStatus: http.StatusUnauthorized,
			wantCode:   apierr.CodeInvalidCredentials,
		},
		{
			name:       "hospital that does not exist is indistinguishable from bad credentials",
			body:       map[string]string{"username": "jsmith", "password": "P@ssw0rd123", "hospital": "hospital-zzz"},
			wantStatus: http.StatusUnauthorized,
			wantCode:   apierr.CodeInvalidCredentials,
		},
		{
			name:       "missing hospital",
			body:       map[string]string{"username": "jsmith", "password": "P@ssw0rd123"},
			wantStatus: http.StatusBadRequest,
			wantCode:   apierr.CodeValidation,
		},
		{
			name:       "empty body",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   apierr.CodeValidation,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := newTestServer(t)
			server.seedStaff(t, hospitalAID, "jsmith", "P@ssw0rd123")

			recorder := server.do(t, http.MethodPost, "/staff/login", tc.body, "")

			assert.Equal(t, tc.wantStatus, recorder.Code, recorder.Body.String())
			assert.Equal(t, tc.wantCode, errorCode(t, recorder))
			assert.NotContains(t, recorder.Body.String(), "access_token")
		})
	}
}

func TestStaff_CreateThenLogin(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	credentials := map[string]string{"username": "jsmith", "password": "P@ssw0rd123", "hospital": "hospital-a"}

	created := server.do(t, http.MethodPost, "/staff/create", credentials, "")
	require.Equal(t, http.StatusCreated, created.Code)

	loggedIn := server.do(t, http.MethodPost, "/staff/login", credentials, "")
	require.Equal(t, http.StatusOK, loggedIn.Code, loggedIn.Body.String())

	token := decode[map[string]any](t, loggedIn)["access_token"].(string)
	claims, err := server.tokens.Parse(token)
	require.NoError(t, err)
	assert.Equal(t, hospitalAID, claims.HospitalID)
}
