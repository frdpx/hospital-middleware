package handler_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bambam/hospital-middleware/internal/apierr"
	"github.com/bambam/hospital-middleware/internal/hisclient"
	"github.com/bambam/hospital-middleware/internal/models"
	"github.com/bambam/hospital-middleware/internal/testutil"
)

func somchai() models.Patient {
	dob, _ := models.ParseDate("1990-05-20")
	return models.Patient{
		NationalID:  testutil.Ptr("1234567890123"),
		FirstNameTH: "สมชาย", LastNameTH: "ใจดี",
		FirstNameEN: "Somchai", LastNameEN: "Jaidee",
		DateOfBirth: &dob,
		PhoneNumber: testutil.Ptr("0812345678"),
		Email:       testutil.Ptr("somchai@example.com"),
		Gender:      models.GenderMale,
	}
}

type searchResponse struct {
	Results []struct {
		PatientHN   string  `json:"patient_hn"`
		NationalID  *string `json:"national_id"`
		PassportID  *string `json:"passport_id"`
		FirstNameEN string  `json:"first_name_en"`
		FirstNameTH string  `json:"first_name_th"`
		LastNameEN  string  `json:"last_name_en"`
		DateOfBirth *string `json:"date_of_birth"`
		Gender      string  `json:"gender"`
	} `json:"results"`
	Count int `json:"count"`
}

// ---------------------------------------------------------------- auth gate

func TestPatientSearch_RequiresAValidToken(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	tests := []struct {
		name   string
		header string
	}{
		{name: "no Authorization header", header: ""},
		{name: "header without the Bearer scheme", header: "some-token"},
		{name: "Basic auth instead of Bearer", header: "Basic dXNlcjpwYXNz"},
		{name: "Bearer with no token", header: "Bearer "},
		{name: "token that is not a JWT", header: "Bearer not-a-jwt"},
		{name: "token signed with another key", header: "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.wrong"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/patient/search", nil)
			req.Header.Set("Content-Type", "application/json")
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			recorder := httptest.NewRecorder()
			server.router.ServeHTTP(recorder, req)

			assert.Equal(t, http.StatusUnauthorized, recorder.Code)
			assert.Equal(t, apierr.CodeUnauthorized, errorCode(t, recorder))
		})
	}
}

func TestPatientSearch_RejectsExpiredToken(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	recorder := server.do(t, http.MethodPost, "/patient/search",
		map[string]string{"national_id": "1234567890123"},
		server.expiredTokenFor(t, hospitalAID))

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.Equal(t, apierr.CodeUnauthorized, errorCode(t, recorder))
}

// ------------------------------------------------------------- local search

func TestPatientSearch_LocalHit(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	server.patients.Seed(hospitalAID, "HN00123", somchai())
	token := server.tokenFor(t, hospitalAID)

	tests := []struct {
		name string
		body map[string]any
	}{
		{name: "by national id", body: map[string]any{"national_id": "1234567890123"}},
		{name: "by first name", body: map[string]any{"first_name": "Somchai"}},
		{name: "by thai last name", body: map[string]any{"last_name": "ใจดี"}},
		{name: "by date of birth", body: map[string]any{"date_of_birth": "1990-05-20"}},
		{name: "by email", body: map[string]any{"email": "somchai@example.com"}},
		{name: "by name and date of birth together", body: map[string]any{"first_name": "Somchai", "date_of_birth": "1990-05-20"}},
		{name: "blank optional fields are ignored, not treated as filters", body: map[string]any{"national_id": "1234567890123", "first_name": "", "email": "  "}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := server.do(t, http.MethodPost, "/patient/search", tc.body, token)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			body := decode[searchResponse](t, recorder)
			require.Equal(t, 1, body.Count)
			require.Len(t, body.Results, 1)

			result := body.Results[0]
			assert.Equal(t, "HN00123", result.PatientHN)
			assert.Equal(t, "Somchai", result.FirstNameEN)
			assert.Equal(t, "สมชาย", result.FirstNameTH)
			require.NotNil(t, result.DateOfBirth)
			assert.Equal(t, "1990-05-20", *result.DateOfBirth, "dates render as YYYY-MM-DD, not RFC3339")
			assert.Nil(t, result.PassportID, "an absent identifier renders as null")
			assert.Equal(t, "M", result.Gender)
		})
	}
}

func TestPatientSearch_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body any
	}{
		{name: "empty object would return the whole hospital", body: `{}`},
		{name: "every field blank is the same as empty", body: map[string]any{"first_name": "", "last_name": "   "}},
		{name: "date of birth in the wrong format", body: map[string]any{"date_of_birth": "20/05/1990"}},
		{name: "date of birth as RFC3339", body: map[string]any{"date_of_birth": "1990-05-20T00:00:00Z"}},
		{name: "malformed JSON", body: `{"national_id": `},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := newTestServer(t)
			token := server.tokenFor(t, hospitalAID)

			recorder := server.do(t, http.MethodPost, "/patient/search", tc.body, token)

			assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			assert.Equal(t, apierr.CodeValidation, errorCode(t, recorder))
		})
	}
}

// A name search with no local match is an empty 200, not an error.
func TestPatientSearch_NoLocalNameMatchReturnsEmptyList(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	token := server.tokenFor(t, hospitalAID)

	recorder := server.do(t, http.MethodPost, "/patient/search",
		map[string]any{"last_name": "Nonexistent"}, token)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := decode[searchResponse](t, recorder)
	assert.Equal(t, 0, body.Count)
	assert.NotNil(t, body.Results, "results must render as [], never null")
	assert.Contains(t, recorder.Body.String(), `"results":[]`)
	assert.Equal(t, 0, server.his.CallCount())
}

// ------------------------------------------------------------ HIS fallback

func TestPatientSearch_FallsBackToHISAndPersists(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	server.his.Add(&hisclient.PatientProfile{Patient: somchai(), PatientHN: "HN00123"})
	token := server.tokenFor(t, hospitalAID)

	recorder := server.do(t, http.MethodPost, "/patient/search",
		map[string]any{"national_id": "1234567890123"}, token)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := decode[searchResponse](t, recorder)
	require.Equal(t, 1, body.Count)
	assert.Equal(t, "HN00123", body.Results[0].PatientHN)
	assert.Equal(t, 1, server.his.CallCount())
	assert.Equal(t, 1, server.patients.UpsertCalls)

	// The record is now local, so the HIS is not consulted again.
	again := server.do(t, http.MethodPost, "/patient/search",
		map[string]any{"national_id": "1234567890123"}, token)
	require.Equal(t, http.StatusOK, again.Code)
	assert.Equal(t, 1, server.his.CallCount())
}

func TestPatientSearch_HISFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		hisErr     error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "patient exists in neither our database nor the HIS",
			hisErr:     hisclient.ErrPatientNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   apierr.CodePatientNotFound,
		},
		{
			name:       "HIS is down",
			hisErr:     errors.New("dial tcp 10.0.0.1:443: connect: connection refused"),
			wantStatus: http.StatusBadGateway,
			wantCode:   apierr.CodeHISUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := newTestServer(t)
			server.his.Err = tc.hisErr
			token := server.tokenFor(t, hospitalAID)

			recorder := server.do(t, http.MethodPost, "/patient/search",
				map[string]any{"national_id": "1234567890123"}, token)

			assert.Equal(t, tc.wantStatus, recorder.Code, recorder.Body.String())
			assert.Equal(t, tc.wantCode, errorCode(t, recorder))
			// Upstream connection details must not reach the client.
			assert.NotContains(t, recorder.Body.String(), "10.0.0.1")
		})
	}
}

// ------------------------------------------------------- cross-hospital rules

// The core access-control rule: a token for Hospital A must never surface a
// patient that belongs to Hospital B.
func TestPatientSearch_NeverLeaksAnotherHospitalsPatient(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	server.patients.Seed(hospitalBID, "B-HN-999", somchai())

	tokenA := server.tokenFor(t, hospitalAID)
	tokenB := server.tokenFor(t, hospitalBID)

	t.Run("hospital A staff searching by name see nothing", func(t *testing.T) {
		recorder := server.do(t, http.MethodPost, "/patient/search",
			map[string]any{"last_name": "Jaidee"}, tokenA)

		require.Equal(t, http.StatusOK, recorder.Code)
		body := decode[searchResponse](t, recorder)
		assert.Equal(t, 0, body.Count)
		assert.NotContains(t, recorder.Body.String(), "B-HN-999")
	})

	t.Run("hospital A staff searching by national id get a 404, not B's record", func(t *testing.T) {
		recorder := server.do(t, http.MethodPost, "/patient/search",
			map[string]any{"national_id": "1234567890123"}, tokenA)

		assert.Equal(t, http.StatusNotFound, recorder.Code)
		assert.Equal(t, apierr.CodePatientNotFound, errorCode(t, recorder))
		assert.NotContains(t, recorder.Body.String(), "B-HN-999")
	})

	t.Run("hospital B staff do see their own patient", func(t *testing.T) {
		recorder := server.do(t, http.MethodPost, "/patient/search",
			map[string]any{"last_name": "Jaidee"}, tokenB)

		require.Equal(t, http.StatusOK, recorder.Code)
		body := decode[searchResponse](t, recorder)
		require.Equal(t, 1, body.Count)
		assert.Equal(t, "B-HN-999", body.Results[0].PatientHN)
	})
}

// The request body carries no hospital field; supplying one must not widen the
// caller's scope beyond the hospital in their token.
func TestPatientSearch_HospitalInBodyCannotOverrideTheTokenScope(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	server.patients.Seed(hospitalBID, "B-HN-999", somchai())
	tokenA := server.tokenFor(t, hospitalAID)

	recorder := server.do(t, http.MethodPost, "/patient/search", map[string]any{
		"last_name":   "Jaidee",
		"hospital":    "hospital-b",
		"hospital_id": hospitalBID.String(),
	}, tokenA)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := decode[searchResponse](t, recorder)
	assert.Equal(t, 0, body.Count)
	assert.NotContains(t, recorder.Body.String(), "B-HN-999")
}

// The same person may exist at both hospitals under different HNs; each side
// sees only its own.
func TestPatientSearch_SamePersonDifferentHNPerHospital(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	shared := somchai()
	shared.ID = uuid.New()
	server.patients.Seed(hospitalAID, "A-HN-001", shared)
	server.patients.Seed(hospitalBID, "B-HN-777", shared)

	fromA := decode[searchResponse](t, server.do(t, http.MethodPost, "/patient/search",
		map[string]any{"national_id": "1234567890123"}, server.tokenFor(t, hospitalAID)))
	fromB := decode[searchResponse](t, server.do(t, http.MethodPost, "/patient/search",
		map[string]any{"national_id": "1234567890123"}, server.tokenFor(t, hospitalBID)))

	require.Equal(t, 1, fromA.Count)
	require.Equal(t, 1, fromB.Count)
	assert.Equal(t, "A-HN-001", fromA.Results[0].PatientHN)
	assert.Equal(t, "B-HN-777", fromB.Results[0].PatientHN)
}
