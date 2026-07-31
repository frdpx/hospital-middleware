package hisclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/frdpx/hospital-middleware/internal/hisclient"
	"github.com/frdpx/hospital-middleware/internal/hisclient/mockhis"
	"github.com/frdpx/hospital-middleware/internal/models"
)

func newMockHIS(t *testing.T, patients ...mockhis.Patient) *hisclient.HospitalAClient {
	t.Helper()

	his := mockhis.NewEmpty()
	for _, p := range patients {
		his.Add(p)
	}
	server := httptest.NewServer(his.Handler())
	t.Cleanup(server.Close)

	return hisclient.NewHospitalAClient(server.URL, server.Client())
}

func TestHospitalAClient_FetchPatientByID_Success(t *testing.T) {
	t.Parallel()

	somchai := mockhis.Patient{
		FirstNameTH: "สมชาย", LastNameTH: "ใจดี",
		FirstNameEN: "Somchai", LastNameEN: "Jaidee",
		DateOfBirth: "1990-05-20", PatientHN: "HN00123",
		NationalID: "1234567890123", PhoneNumber: "0812345678",
		Email: "somchai@example.com", Gender: "M",
	}
	john := mockhis.Patient{
		FirstNameEN: "John", LastNameEN: "Doe",
		DateOfBirth: "1978-01-15", PatientHN: "HN00789",
		PassportID: "AA1234567", Gender: "male",
	}

	tests := []struct {
		name      string
		lookupID  string
		assertion func(t *testing.T, got *hisclient.PatientProfile)
	}{
		{
			name:     "lookup by national id returns a fully normalized patient",
			lookupID: "1234567890123",
			assertion: func(t *testing.T, got *hisclient.PatientProfile) {
				assert.Equal(t, "HN00123", got.PatientHN)
				require.NotNil(t, got.Patient.NationalID)
				assert.Equal(t, "1234567890123", *got.Patient.NationalID)
				assert.Nil(t, got.Patient.PassportID, "absent identifier must be NULL, not empty string")
				assert.Equal(t, "Somchai", got.Patient.FirstNameEN)
				assert.Equal(t, "สมชาย", got.Patient.FirstNameTH)
				assert.Nil(t, got.Patient.MiddleNameEN)
				require.NotNil(t, got.Patient.DateOfBirth)
				assert.Equal(t, "1990-05-20", got.Patient.DateOfBirth.String())
				assert.Equal(t, models.GenderMale, got.Patient.Gender)
			},
		},
		{
			name:     "lookup by passport id works for patients with no national id",
			lookupID: "AA1234567",
			assertion: func(t *testing.T, got *hisclient.PatientProfile) {
				assert.Equal(t, "HN00789", got.PatientHN)
				require.NotNil(t, got.Patient.PassportID)
				assert.Equal(t, "AA1234567", *got.Patient.PassportID)
				assert.Nil(t, got.Patient.NationalID)
				assert.Equal(t, models.GenderMale, got.Patient.Gender, "spelled-out gender is normalized")
			},
		},
	}

	client := newMockHIS(t, somchai, john)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := client.FetchPatientByID(context.Background(), tc.lookupID)

			require.NoError(t, err)
			require.NotNil(t, got)
			tc.assertion(t, got)
		})
	}
}

func TestHospitalAClient_FetchPatientByID_UpstreamFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		lookupID   string
		wantErrIs  error
		wantErrMsg string
	}{
		{
			name: "404 from HIS means the patient does not exist there",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			lookupID:  "1234567890123",
			wantErrIs: hisclient.ErrPatientNotFound,
		},
		{
			name: "500 from HIS is a transport failure, not a missing patient",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			lookupID:   "1234567890123",
			wantErrIs:  hisclient.ErrUnavailable,
			wantErrMsg: "status 500",
		},
		{
			name: "401 from HIS is surfaced as unavailable rather than leaking upstream auth state",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			lookupID:  "1234567890123",
			wantErrIs: hisclient.ErrUnavailable,
		},
		{
			name: "malformed JSON body is unavailable",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"first_name_en": `))
			},
			lookupID:   "1234567890123",
			wantErrIs:  hisclient.ErrUnavailable,
			wantErrMsg: "decode",
		},
		{
			name: "response with no identifier at all cannot be stored",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"first_name_en":"Nobody","patient_hn":"HN1"}`))
			},
			lookupID:   "1234567890123",
			wantErrIs:  hisclient.ErrUnavailable,
			wantErrMsg: "neither national_id nor passport_id",
		},
		{
			name: "response with no patient_hn cannot be linked to a hospital",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"national_id":"1234567890123","first_name_en":"Nobody"}`))
			},
			lookupID:   "1234567890123",
			wantErrIs:  hisclient.ErrUnavailable,
			wantErrMsg: "no patient_hn",
		},
		{
			name: "unparseable date_of_birth is rejected",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"national_id":"1","patient_hn":"HN1","date_of_birth":"20 May 1990"}`))
			},
			lookupID:   "1234567890123",
			wantErrIs:  hisclient.ErrUnavailable,
			wantErrMsg: "date_of_birth",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(tc.handler)
			t.Cleanup(server.Close)
			client := hisclient.NewHospitalAClient(server.URL, server.Client())

			got, err := client.FetchPatientByID(context.Background(), tc.lookupID)

			assert.Nil(t, got)
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.wantErrIs)
			if tc.wantErrMsg != "" {
				assert.Contains(t, err.Error(), tc.wantErrMsg)
			}
		})
	}
}

func TestHospitalAClient_FetchPatientByID_EmptyIdentifier(t *testing.T) {
	t.Parallel()

	client := newMockHIS(t)

	got, err := client.FetchPatientByID(context.Background(), "   ")

	assert.Nil(t, got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty patient identifier")
}

func TestHospitalAClient_FetchPatientByID_EscapesIdentifierInPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	client := hisclient.NewHospitalAClient(server.URL, server.Client())

	_, err := client.FetchPatientByID(context.Background(), "../../admin/all")

	assert.ErrorIs(t, err, hisclient.ErrPatientNotFound)
	assert.Equal(t, "/patient/search/..%2F..%2Fadmin%2Fall", gotPath)
}

func TestHospitalAClient_FetchPatientByID_TimesOut(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	client := hisclient.NewHospitalAClient(server.URL, &http.Client{Timeout: 50 * time.Millisecond})

	_, err := client.FetchPatientByID(context.Background(), "1234567890123")

	require.Error(t, err)
	assert.ErrorIs(t, err, hisclient.ErrUnavailable)
}

func TestHospitalAClient_FetchPatientByID_HonoursContextCancellation(t *testing.T) {
	t.Parallel()

	client := newMockHIS(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.FetchPatientByID(ctx, "1234567890123")

	require.Error(t, err)
	assert.ErrorIs(t, err, hisclient.ErrUnavailable)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestDefaultFactory_ClientFor(t *testing.T) {
	t.Parallel()

	baseURL := "https://hospital-a.api.co.th"

	tests := []struct {
		name       string
		override   string
		hospital   models.Hospital
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:     "resolves the hospital_a adapter from the hospital's own base URL",
			hospital: models.Hospital{Code: "hospital-a", HISAdapterType: models.HISAdapterHospitalA, HISBaseURL: &baseURL},
		},
		{
			name:     "base URL override wins over the stored one",
			override: "http://localhost:9090",
			hospital: models.Hospital{Code: "hospital-a", HISAdapterType: models.HISAdapterHospitalA, HISBaseURL: &baseURL},
		},
		{
			name:       "hospital with no HIS URL configured is an error, not a nil client",
			hospital:   models.Hospital{Code: "hospital-z", HISAdapterType: models.HISAdapterHospitalA},
			wantErr:    true,
			wantErrMsg: "no HIS base URL",
		},
		{
			name:       "unknown adapter type is rejected",
			hospital:   models.Hospital{Code: "hospital-x", HISAdapterType: "hospital_x", HISBaseURL: &baseURL},
			wantErr:    true,
			wantErrMsg: "no HIS adapter registered",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			factory := hisclient.NewDefaultFactory(http.DefaultClient, tc.override)

			client, err := factory.ClientFor(tc.hospital)

			if tc.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, hisclient.ErrUnavailable)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				assert.Nil(t, client)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, client)
		})
	}
}
