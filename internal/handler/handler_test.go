package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/bambam/hospital-middleware/internal/auth"
	"github.com/bambam/hospital-middleware/internal/config"
	"github.com/bambam/hospital-middleware/internal/handler"
	"github.com/bambam/hospital-middleware/internal/models"
	"github.com/bambam/hospital-middleware/internal/service"
	"github.com/bambam/hospital-middleware/internal/testutil"
)

var (
	hospitalAID = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	hospitalBID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

func init() { gin.SetMode(gin.TestMode) }

type testServer struct {
	router   *gin.Engine
	tokens   *auth.TokenManager
	staff    *testutil.StaffRepo
	patients *testutil.PatientRepo
	his      *testutil.FakeHIS
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	hospitals := testutil.NewHospitalRepo(
		models.Hospital{ID: hospitalAID, Code: "hospital-a", Name: "Hospital A", HISAdapterType: models.HISAdapterHospitalA},
		models.Hospital{ID: hospitalBID, Code: "hospital-b", Name: "Hospital B", HISAdapterType: models.HISAdapterHospitalA},
	)
	staffRepo := testutil.NewStaffRepo()
	patientRepo := testutil.NewPatientRepo()
	fakeHIS := testutil.NewFakeHIS()

	tokens := auth.NewTokenManager(config.JWTConfig{
		Secret: "test-secret-that-is-long-enough-32b",
		Issuer: "hospital-middleware",
		TTL:    time.Hour,
	})

	staffService := service.NewStaffService(hospitals, staffRepo, tokens).WithBcryptCost(bcrypt.MinCost)
	patientService := service.NewPatientService(hospitals, patientRepo, testutil.NewFakeHISFactory(fakeHIS), logger)

	router := handler.NewRouter(handler.RouterDeps{
		Staff:    handler.NewStaffHandler(staffService, logger),
		Patients: handler.NewPatientHandler(patientService, logger),
		Tokens:   tokens,
		Logger:   logger,
		Ping:     func(context.Context) error { return nil },
	})

	return &testServer{
		router:   router,
		tokens:   tokens,
		staff:    staffRepo,
		patients: patientRepo,
		his:      fakeHIS,
	}
}

func (s *testServer) do(t *testing.T, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	switch v := body.(type) {
	case nil:
		reader = nil
	case string:
		reader = bytes.NewBufferString(v)
	default:
		encoded, err := json.Marshal(v)
		require.NoError(t, err)
		reader = bytes.NewBuffer(encoded)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	recorder := httptest.NewRecorder()
	s.router.ServeHTTP(recorder, req)
	return recorder
}

func (s *testServer) seedStaff(t *testing.T, hospitalID uuid.UUID, username, password string) models.Staff {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)

	created, err := s.staff.Create(context.Background(), &models.Staff{
		HospitalID: hospitalID, Username: username, PasswordHash: string(hash),
	})
	require.NoError(t, err)
	return *created
}

func (s *testServer) tokenFor(t *testing.T, hospitalID uuid.UUID) string {
	t.Helper()

	token, _, err := s.tokens.Generate(&models.Staff{
		ID: uuid.New(), HospitalID: hospitalID, Username: "tester",
	})
	require.NoError(t, err)
	return token
}

func (s *testServer) expiredTokenFor(t *testing.T, hospitalID uuid.UUID) string {
	t.Helper()

	past := s.tokens.WithClock(func() time.Time { return time.Now().Add(-2 * time.Hour) })
	token, _, err := past.Generate(&models.Staff{
		ID: uuid.New(), HospitalID: hospitalID, Username: "tester",
	})
	require.NoError(t, err)
	return token
}

func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body),
		"every error response must use the shared envelope: %s", recorder.Body.String())
	return body.Error.Code
}

func decode[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()

	var out T
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &out), "body: %s", recorder.Body.String())
	return out
}

func TestRouter_HealthAndReadiness(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	for _, path := range []string{"/healthz", "/readyz"} {
		recorder := server.do(t, http.MethodGet, path, nil, "")
		require.Equal(t, http.StatusOK, recorder.Code, path)
	}
}

func TestRouter_RecoversFromAPanic(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	server.router.GET("/boom", func(*gin.Context) {
		panic("something went very wrong")
	})

	recorder := server.do(t, http.MethodGet, "/boom", nil, "")

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, "INTERNAL_ERROR", errorCode(t, recorder))

	require.NotContains(t, recorder.Body.String(), "something went very wrong")

	after := server.do(t, http.MethodGet, "/healthz", nil, "")
	require.Equal(t, http.StatusOK, after.Code)
}

func TestRouter_UnknownRouteAndMethod(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	notFound := server.do(t, http.MethodPost, "/does/not/exist", nil, "")
	require.Equal(t, http.StatusNotFound, notFound.Code)
	require.Equal(t, "ROUTE_NOT_FOUND", errorCode(t, notFound))

	wrongMethod := server.do(t, http.MethodGet, "/staff/login", nil, "")
	require.Equal(t, http.StatusMethodNotAllowed, wrongMethod.Code)
	require.Equal(t, "METHOD_NOT_ALLOWED", errorCode(t, wrongMethod))
}
