package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bambam/hospital-middleware/internal/apierr"
	"github.com/bambam/hospital-middleware/internal/hisclient"
	"github.com/bambam/hospital-middleware/internal/models"
	"github.com/bambam/hospital-middleware/internal/service"
	"github.com/bambam/hospital-middleware/internal/testutil"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

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

// newPatientService wires the service with in-memory repos and a fake HIS.
func newPatientService(t *testing.T) (*service.PatientService, *testutil.PatientRepo, *testutil.FakeHIS) {
	t.Helper()

	patientRepo := testutil.NewPatientRepo()
	fakeHIS := testutil.NewFakeHIS()
	svc := service.NewPatientService(
		testutil.NewHospitalRepo(testHospitals()...),
		patientRepo,
		testutil.NewFakeHISFactory(fakeHIS),
		discardLogger(),
	)
	return svc, patientRepo, fakeHIS
}

func TestPatientService_Search_LocalHit(t *testing.T) {
	t.Parallel()

	patient := somchai()
	dob, _ := models.ParseDate("1990-05-20")

	tests := []struct {
		name     string
		criteria models.PatientSearchCriteria
	}{
		{name: "by national id", criteria: models.PatientSearchCriteria{NationalID: testutil.Ptr("1234567890123")}},
		{name: "by english first name", criteria: models.PatientSearchCriteria{FirstName: testutil.Ptr("Somchai")}},
		{name: "by thai first name", criteria: models.PatientSearchCriteria{FirstName: testutil.Ptr("สมชาย")}},
		{name: "by partial name, case-insensitively", criteria: models.PatientSearchCriteria{LastName: testutil.Ptr("jaid")}},
		{name: "by date of birth", criteria: models.PatientSearchCriteria{DateOfBirth: &dob}},
		{name: "by phone number", criteria: models.PatientSearchCriteria{PhoneNumber: testutil.Ptr("0812345678")}},
		{name: "by email, case-insensitively", criteria: models.PatientSearchCriteria{Email: testutil.Ptr("SOMCHAI@EXAMPLE.COM")}},
		{
			name: "by several criteria at once",
			criteria: models.PatientSearchCriteria{
				FirstName: testutil.Ptr("Somchai"), LastName: testutil.Ptr("Jaidee"), DateOfBirth: &dob,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, fakeHIS := newPatientService(t)
			repo.Seed(hospitalAID, "HN00123", patient)

			criteria := tc.criteria
			criteria.HospitalID = hospitalAID

			records, err := svc.Search(context.Background(), criteria)

			require.NoError(t, err)
			require.Len(t, records, 1)
			assert.Equal(t, "HN00123", records[0].PatientHN)
			assert.Equal(t, "Somchai", records[0].FirstNameEN)
			assert.Equal(t, 0, fakeHIS.CallCount(), "a local hit must not trigger a HIS call")
		})
	}
}

func TestPatientService_Search_Rejections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		criteria models.PatientSearchCriteria
		wantCode string
	}{
		{
			name:     "no criteria at all would dump the whole hospital",
			criteria: models.PatientSearchCriteria{HospitalID: hospitalAID},
			wantCode: apierr.CodeValidation,
		},
		{
			name:     "no hospital scope",
			criteria: models.PatientSearchCriteria{NationalID: testutil.Ptr("1234567890123")},
			wantCode: apierr.CodeUnauthorized,
		},
		{
			name: "hospital in the token no longer exists",
			criteria: models.PatientSearchCriteria{
				HospitalID: uuid.MustParse("99999999-9999-4999-8999-999999999999"),
				NationalID: testutil.Ptr("1234567890123"),
			},
			wantCode: apierr.CodeHospitalNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, _, _ := newPatientService(t)

			records, err := svc.Search(context.Background(), tc.criteria)

			assert.Nil(t, records)
			require.Error(t, err)
			assert.Equal(t, tc.wantCode, apierr.From(err).Code)
		})
	}
}

// Name matching is a substring match, so a one-character filter would return
// most of the hospital's roster — national ids included — up to the result
// limit. The length is counted in runes: a Thai character is three bytes, and a
// byte-based check would reject "สม" while happily accepting "ab".
func TestPatientService_Search_RejectsTooShortNameFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		criteria models.PatientSearchCriteria
		wantErr  bool
	}{
		{
			name:     "single latin character",
			criteria: models.PatientSearchCriteria{LastName: testutil.Ptr("a")},
			wantErr:  true,
		},
		{
			name:     "single thai character",
			criteria: models.PatientSearchCriteria{FirstName: testutil.Ptr("ส")},
			wantErr:  true,
		},
		{
			name:     "single character in a middle name",
			criteria: models.PatientSearchCriteria{MiddleName: testutil.Ptr("x")},
			wantErr:  true,
		},
		{
			name:     "two latin characters are allowed",
			criteria: models.PatientSearchCriteria{LastName: testutil.Ptr("ja")},
		},
		{
			name:     "two thai characters are allowed, not treated as six bytes",
			criteria: models.PatientSearchCriteria{FirstName: testutil.Ptr("สม")},
		},
		{
			name:     "the limit applies to names only, not to identifiers",
			criteria: models.PatientSearchCriteria{NationalID: testutil.Ptr("1")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, fakeHIS := newPatientService(t)
			repo.Seed(hospitalAID, "HN00123", somchai())
			fakeHIS.Err = hisclient.ErrPatientNotFound // identifier lookups may miss

			criteria := tc.criteria
			criteria.HospitalID = hospitalAID

			_, err := svc.Search(context.Background(), criteria)

			if tc.wantErr {
				require.Error(t, err)
				assert.Equal(t, apierr.CodeValidation, apierr.From(err).Code)
				assert.Contains(t, apierr.From(err).Message, "at least 2 characters")
				return
			}
			// Anything but a validation error is fine here; these cases are
			// only asserting that the length gate lets them through.
			if err != nil {
				assert.NotEqual(t, apierr.CodeValidation, apierr.From(err).Code)
			}
		})
	}
}

// A name/phone/email search that finds nothing is an empty result, not a 404
// and not a HIS call: the HIS exposes no way to search by those fields.
func TestPatientService_Search_NonIdentifierMissIsEmptyAndSkipsHIS(t *testing.T) {
	t.Parallel()

	svc, _, fakeHIS := newPatientService(t)

	records, err := svc.Search(context.Background(), models.PatientSearchCriteria{
		HospitalID: hospitalAID,
		LastName:   testutil.Ptr("Nonexistent"),
	})

	require.NoError(t, err)
	assert.Empty(t, records)
	assert.Equal(t, 0, fakeHIS.CallCount())
}

func TestPatientService_Search_FallsBackToHIS(t *testing.T) {
	t.Parallel()

	passportPatient := models.Patient{
		PassportID:  testutil.Ptr("AA1234567"),
		FirstNameEN: "John", LastNameEN: "Doe",
		Gender: models.GenderMale,
	}

	tests := []struct {
		name       string
		profile    *hisclient.PatientProfile
		criteria   models.PatientSearchCriteria
		wantHN     string
		wantLookup string
	}{
		{
			name:       "national id lookup",
			profile:    &hisclient.PatientProfile{Patient: somchai(), PatientHN: "HN00123"},
			criteria:   models.PatientSearchCriteria{NationalID: testutil.Ptr("1234567890123")},
			wantHN:     "HN00123",
			wantLookup: "1234567890123",
		},
		{
			name:       "passport id lookup",
			profile:    &hisclient.PatientProfile{Patient: passportPatient, PatientHN: "HN00789"},
			criteria:   models.PatientSearchCriteria{PassportID: testutil.Ptr("AA1234567")},
			wantHN:     "HN00789",
			wantLookup: "AA1234567",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, fakeHIS := newPatientService(t)
			fakeHIS.Add(tc.profile)

			criteria := tc.criteria
			criteria.HospitalID = hospitalAID

			records, err := svc.Search(context.Background(), criteria)

			require.NoError(t, err)
			require.Len(t, records, 1)
			assert.Equal(t, tc.wantHN, records[0].PatientHN)
			assert.Equal(t, []string{tc.wantLookup}, fakeHIS.Calls)
			assert.Equal(t, 1, repo.UpsertCalls, "the HIS result must be persisted, not just returned")

			// A second identical search is served locally.
			_, err = svc.Search(context.Background(), criteria)
			require.NoError(t, err)
			assert.Equal(t, 1, fakeHIS.CallCount(), "the second search must be served from local storage")
		})
	}
}

func TestPatientService_Search_HISFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		hisErr     error
		wantCode   string
		wantUpsert int
	}{
		{
			name:     "HIS has no such patient either",
			hisErr:   hisclient.ErrPatientNotFound,
			wantCode: apierr.CodePatientNotFound,
		},
		{
			name:     "HIS is unreachable",
			hisErr:   errors.New("dial tcp: connection refused"),
			wantCode: apierr.CodeHISUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, fakeHIS := newPatientService(t)
			fakeHIS.Err = tc.hisErr

			records, err := svc.Search(context.Background(), models.PatientSearchCriteria{
				HospitalID: hospitalAID,
				NationalID: testutil.Ptr("1234567890123"),
			})

			assert.Nil(t, records)
			require.Error(t, err)
			assert.Equal(t, tc.wantCode, apierr.From(err).Code)
			assert.Equal(t, tc.wantUpsert, repo.UpsertCalls, "a failed HIS lookup must not write anything")
		})
	}
}

func TestPatientService_Search_NoHISAdapterConfigured(t *testing.T) {
	t.Parallel()

	factory := testutil.NewFakeHISFactory(testutil.NewFakeHIS())
	factory.Err = errors.New("no adapter for hospital")

	svc := service.NewPatientService(
		testutil.NewHospitalRepo(testHospitals()...),
		testutil.NewPatientRepo(),
		factory,
		discardLogger(),
	)

	_, err := svc.Search(context.Background(), models.PatientSearchCriteria{
		HospitalID: hospitalAID,
		NationalID: testutil.Ptr("1234567890123"),
	})

	require.Error(t, err)
	assert.Equal(t, apierr.CodeHISUnavailable, apierr.From(err).Code)
}

func TestPatientService_Search_UpsertFailureSurfacesAsInternal(t *testing.T) {
	t.Parallel()

	svc, repo, fakeHIS := newPatientService(t)
	fakeHIS.Add(&hisclient.PatientProfile{Patient: somchai(), PatientHN: "HN00123"})
	repo.UpsertErr = apierr.Internal().WithCause(errors.New("deadlock detected"))

	records, err := svc.Search(context.Background(), models.PatientSearchCriteria{
		HospitalID: hospitalAID,
		NationalID: testutil.Ptr("1234567890123"),
	})

	assert.Nil(t, records)
	require.Error(t, err)
	assert.Equal(t, apierr.CodeInternal, apierr.From(err).Code)
	assert.NotContains(t, apierr.From(err).Message, "deadlock")
}

// The HIS answers on identifier alone. If the caller also supplied a name that
// does not match the fetched patient, the re-run scoped query must filter them
// out rather than returning a patient the caller did not ask for.
func TestPatientService_Search_HISResultStillHonoursOtherCriteria(t *testing.T) {
	t.Parallel()

	svc, repo, fakeHIS := newPatientService(t)
	fakeHIS.Add(&hisclient.PatientProfile{Patient: somchai(), PatientHN: "HN00123"})

	records, err := svc.Search(context.Background(), models.PatientSearchCriteria{
		HospitalID: hospitalAID,
		NationalID: testutil.Ptr("1234567890123"),
		LastName:   testutil.Ptr("Wrongname"),
	})

	assert.Nil(t, records)
	require.Error(t, err)
	assert.Equal(t, apierr.CodePatientNotFound, apierr.From(err).Code)
	assert.Equal(t, 1, repo.UpsertCalls, "the patient is still synced; they are just not a match")
}

// Regression: an identifier that is already on file for this hospital must not
// send us back to the HIS just because some *other* criterion did not match.
// Before this check, every such request cost one upstream call and one write
// transaction and still returned 404 — repeatable without limit.
func TestPatientService_Search_KnownIdentifierNeverRefetches(t *testing.T) {
	t.Parallel()

	svc, repo, fakeHIS := newPatientService(t)
	repo.Seed(hospitalAID, "HN00123", somchai())
	fakeHIS.Add(&hisclient.PatientProfile{Patient: somchai(), PatientHN: "HN00123"})

	for i := range 3 {
		records, err := svc.Search(context.Background(), models.PatientSearchCriteria{
			HospitalID: hospitalAID,
			NationalID: testutil.Ptr("1234567890123"),
			LastName:   testutil.Ptr("Wrongname"),
		})

		assert.Nil(t, records, "attempt %d", i+1)
		require.Error(t, err)
		assert.Equal(t, apierr.CodePatientNotFound, apierr.From(err).Code)
	}

	assert.Equal(t, 0, fakeHIS.CallCount(), "the patient is already on file; the HIS must not be consulted")
	assert.Equal(t, 0, repo.UpsertCalls, "and nothing must be written")
}

// Regression: a hospital whose HIS holds a thinner record of the same person
// must not erase what another hospital's HIS already supplied. `patients` is
// shared across hospitals, so a plain overwrite is cross-tenant data loss.
func TestPatientService_Search_ResyncDoesNotEraseKnownFields(t *testing.T) {
	t.Parallel()

	svc, repo, fakeHIS := newPatientService(t)

	// Hospital A knows this person in full.
	rich := somchai()
	rich.MiddleNameEN = testutil.Ptr("Somsak")
	rich.MiddleNameTH = testutil.Ptr("สมศักดิ์")
	rich.Email = testutil.Ptr("rich@example.com")
	repo.Seed(hospitalAID, "A-HN-001", rich)

	// Hospital B's HIS returns the same person with less detail.
	thin := models.Patient{
		NationalID:  testutil.Ptr("1234567890123"),
		FirstNameEN: "Somchai",
		LastNameEN:  "Jaidee",
		Gender:      models.GenderMale,
	}
	fakeHIS.Add(&hisclient.PatientProfile{Patient: thin, PatientHN: "B-HN-777"})

	// Hospital B has no link yet, so this search syncs from B's HIS.
	fromB, err := svc.Search(context.Background(), models.PatientSearchCriteria{
		HospitalID: hospitalBID,
		NationalID: testutil.Ptr("1234567890123"),
	})
	require.NoError(t, err)
	require.Len(t, fromB, 1)
	assert.Equal(t, "B-HN-777", fromB[0].PatientHN)

	// Hospital A must still see everything it knew before B ever searched.
	fromA, err := svc.Search(context.Background(), models.PatientSearchCriteria{
		HospitalID: hospitalAID,
		NationalID: testutil.Ptr("1234567890123"),
	})
	require.NoError(t, err)
	require.Len(t, fromA, 1)

	require.NotNil(t, fromA[0].MiddleNameEN, "middle_name_en was erased by the thinner re-sync")
	assert.Equal(t, "Somsak", *fromA[0].MiddleNameEN)
	require.NotNil(t, fromA[0].MiddleNameTH)
	assert.Equal(t, "สมศักดิ์", *fromA[0].MiddleNameTH)
	require.NotNil(t, fromA[0].Email)
	assert.Equal(t, "rich@example.com", *fromA[0].Email)
	require.NotNil(t, fromA[0].DateOfBirth, "date_of_birth was erased")
	assert.Equal(t, "1990-05-20", fromA[0].DateOfBirth.String())
	assert.Equal(t, "A-HN-001", fromA[0].PatientHN, "each hospital keeps its own HN")
}

// ------------------------------------------------------- cross-hospital rules

// A patient registered only at Hospital B must be invisible to Hospital A's
// staff, even though both hospitals share the same `patients` table.
func TestPatientService_Search_NeverReturnsAnotherHospitalsPatient(t *testing.T) {
	t.Parallel()

	svc, repo, fakeHIS := newPatientService(t)
	repo.Seed(hospitalBID, "B-HN-999", somchai())

	t.Run("name search finds nothing and does not reach for the HIS", func(t *testing.T) {
		records, err := svc.Search(context.Background(), models.PatientSearchCriteria{
			HospitalID: hospitalAID,
			LastName:   testutil.Ptr("Jaidee"),
		})

		require.NoError(t, err)
		assert.Empty(t, records)
		assert.Equal(t, 0, fakeHIS.CallCount())
	})

	t.Run("identifier search does not fall through to the other hospital's row", func(t *testing.T) {
		// Hospital A's own HIS does not know this patient either.
		records, err := svc.Search(context.Background(), models.PatientSearchCriteria{
			HospitalID: hospitalAID,
			NationalID: testutil.Ptr("1234567890123"),
		})

		assert.Nil(t, records)
		require.Error(t, err)
		assert.Equal(t, apierr.CodePatientNotFound, apierr.From(err).Code)
	})

	t.Run("the owning hospital still sees their own patient", func(t *testing.T) {
		records, err := svc.Search(context.Background(), models.PatientSearchCriteria{
			HospitalID: hospitalBID,
			LastName:   testutil.Ptr("Jaidee"),
		})

		require.NoError(t, err)
		require.Len(t, records, 1)
		assert.Equal(t, "B-HN-999", records[0].PatientHN)
	})
}

// The same person may be registered at two hospitals with different HNs; each
// hospital's staff must see only their own HN.
func TestPatientService_Search_SamePersonDifferentHNPerHospital(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newPatientService(t)
	shared := somchai()
	shared.ID = uuid.New()
	repo.Seed(hospitalAID, "A-HN-001", shared)
	repo.Seed(hospitalBID, "B-HN-777", shared)

	fromA, err := svc.Search(context.Background(), models.PatientSearchCriteria{
		HospitalID: hospitalAID, NationalID: testutil.Ptr("1234567890123"),
	})
	require.NoError(t, err)
	fromB, err := svc.Search(context.Background(), models.PatientSearchCriteria{
		HospitalID: hospitalBID, NationalID: testutil.Ptr("1234567890123"),
	})
	require.NoError(t, err)

	require.Len(t, fromA, 1)
	require.Len(t, fromB, 1)
	assert.Equal(t, "A-HN-001", fromA[0].PatientHN)
	assert.Equal(t, "B-HN-777", fromB[0].PatientHN)
	assert.Equal(t, fromA[0].ID, fromB[0].ID, "it is the same canonical person behind both HNs")
}
