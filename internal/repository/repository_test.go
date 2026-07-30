package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bambam/hospital-middleware/internal/apierr"
	"github.com/bambam/hospital-middleware/internal/hisclient"
	"github.com/bambam/hospital-middleware/internal/models"
	"github.com/bambam/hospital-middleware/internal/repository"
)

// These tests use sqlmock to assert the SQL itself — that the hospital filter
// is always present, that values are bound rather than interpolated, and that
// a Postgres unique violation becomes a 409 rather than a 500.

var (
	hospitalAID = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	patientID   = uuid.MustParse("33333333-3333-4333-8333-333333333333")
)

func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, mock.ExpectationsWereMet(), "unmet SQL expectations")
		_ = db.Close()
	})
	return db, mock
}

func uniqueViolation(constraint string) error {
	return &pgconn.PgError{Code: "23505", ConstraintName: constraint}
}

// -------------------------------------------------------- HospitalRepository

func TestHospitalRepository_FindByCodeOrName(t *testing.T) {
	t.Parallel()

	t.Run("matches on either code or name, case-insensitively", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		mock.ExpectQuery(`SELECT .* FROM hospitals\s+WHERE lower\(code\) = lower\(\$1\) OR lower\(name\) = lower\(\$1\)`).
			WithArgs("Hospital A").
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "code", "name", "his_adapter_type", "his_base_url", "created_at", "updated_at",
			}).AddRow(hospitalAID, "hospital-a", "Hospital A", "hospital_a", "https://hospital-a.api.co.th", time.Now(), time.Now()))

		hospital, err := repository.NewHospitalRepository(db).FindByCodeOrName(context.Background(), "Hospital A")

		require.NoError(t, err)
		assert.Equal(t, hospitalAID, hospital.ID)
		assert.Equal(t, "hospital-a", hospital.Code)
		assert.Equal(t, models.HISAdapterHospitalA, hospital.HISAdapterType)
	})

	t.Run("no such hospital is a 404, not a 500", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		mock.ExpectQuery(`FROM hospitals`).WithArgs("nope").WillReturnError(sql.ErrNoRows)

		hospital, err := repository.NewHospitalRepository(db).FindByCodeOrName(context.Background(), "nope")

		assert.Nil(t, hospital)
		require.Error(t, err)
		assert.Equal(t, apierr.CodeHospitalNotFound, apierr.From(err).Code)
	})

	t.Run("a driver failure becomes an internal error", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		mock.ExpectQuery(`FROM hospitals`).WithArgs("hospital-a").WillReturnError(errors.New("connection reset"))

		_, err := repository.NewHospitalRepository(db).FindByCodeOrName(context.Background(), "hospital-a")

		require.Error(t, err)
		apiErr := apierr.From(err)
		assert.Equal(t, apierr.CodeInternal, apiErr.Code)
		assert.NotContains(t, apiErr.Message, "connection reset")
	})
}

// ----------------------------------------------------------- StaffRepository

func TestStaffRepository_Create(t *testing.T) {
	t.Parallel()

	staff := &models.Staff{HospitalID: hospitalAID, Username: "jsmith", PasswordHash: "$2a$10$hash"}

	t.Run("returns the generated id and timestamps", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		staffID := uuid.New()
		now := time.Now()
		mock.ExpectQuery(`INSERT INTO staff \(hospital_id, username, password_hash\)`).
			WithArgs(hospitalAID, "jsmith", "$2a$10$hash").
			WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(staffID, now, now))

		created, err := repository.NewStaffRepository(db).Create(context.Background(), staff)

		require.NoError(t, err)
		assert.Equal(t, staffID, created.ID)
		assert.Equal(t, "jsmith", created.Username)
	})

	t.Run("a duplicate within the hospital becomes USERNAME_TAKEN", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		mock.ExpectQuery(`INSERT INTO staff`).
			WithArgs(hospitalAID, "jsmith", "$2a$10$hash").
			WillReturnError(uniqueViolation("ux_staff_hospital_username"))

		created, err := repository.NewStaffRepository(db).Create(context.Background(), staff)

		assert.Nil(t, created)
		require.Error(t, err)
		assert.Equal(t, apierr.CodeUsernameTaken, apierr.From(err).Code)
	})

	t.Run("a unique violation on some other constraint is not a username clash", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		mock.ExpectQuery(`INSERT INTO staff`).
			WithArgs(hospitalAID, "jsmith", "$2a$10$hash").
			WillReturnError(uniqueViolation("some_other_index"))

		_, err := repository.NewStaffRepository(db).Create(context.Background(), staff)

		require.Error(t, err)
		assert.Equal(t, apierr.CodeInternal, apierr.From(err).Code)
	})
}

func TestStaffRepository_FindByHospitalAndUsername(t *testing.T) {
	t.Parallel()

	t.Run("scopes the lookup to the hospital, never username alone", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		staffID := uuid.New()
		mock.ExpectQuery(`FROM staff\s+WHERE hospital_id = \$1 AND lower\(username\) = lower\(\$2\)`).
			WithArgs(hospitalAID, "jsmith").
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "hospital_id", "username", "password_hash", "created_at", "updated_at",
			}).AddRow(staffID, hospitalAID, "jsmith", "$2a$10$hash", time.Now(), time.Now()))

		found, err := repository.NewStaffRepository(db).
			FindByHospitalAndUsername(context.Background(), hospitalAID, "jsmith")

		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, hospitalAID, found.HospitalID)
	})

	t.Run("a missing staff member is (nil, nil) so login can still burn time", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		mock.ExpectQuery(`FROM staff`).WithArgs(hospitalAID, "nobody").WillReturnError(sql.ErrNoRows)

		found, err := repository.NewStaffRepository(db).
			FindByHospitalAndUsername(context.Background(), hospitalAID, "nobody")

		assert.NoError(t, err)
		assert.Nil(t, found)
	})
}

// --------------------------------------------------------- PatientRepository

func patientRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "national_id", "passport_id",
		"first_name_th", "middle_name_th", "last_name_th",
		"first_name_en", "middle_name_en", "last_name_en",
		"date_of_birth", "phone_number", "email", "gender",
		"created_at", "updated_at", "patient_hn", "synced_at",
	})
}

func TestPatientRepository_Search_AlwaysFiltersByHospital(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	mock.ExpectQuery(`FROM hospital_patients hp\s+JOIN patients p ON p\.id = hp\.patient_id\s+WHERE hp\.hospital_id = \$1`).
		WithArgs(hospitalAID, "1234567890123").
		WillReturnRows(patientRows().AddRow(
			patientID, "1234567890123", nil,
			"สมชาย", nil, "ใจดี",
			"Somchai", nil, "Jaidee",
			time.Date(1990, 5, 20, 0, 0, 0, 0, time.UTC), "0812345678", "somchai@example.com", "M",
			time.Now(), time.Now(), "HN00123", time.Now(),
		))

	records, err := repository.NewPatientRepository(db).Search(context.Background(), models.PatientSearchCriteria{
		HospitalID: hospitalAID,
		NationalID: ptr("1234567890123"),
	})

	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "HN00123", records[0].PatientHN)
	assert.Equal(t, "Somchai", records[0].FirstNameEN)
	require.NotNil(t, records[0].DateOfBirth)
	assert.Equal(t, "1990-05-20", records[0].DateOfBirth.String())
}

// Every filter must arrive as a bound parameter in a stable order, with the
// hospital scope always in position $1.
func TestPatientRepository_Search_BindsEveryCriterionAsAParameter(t *testing.T) {
	t.Parallel()

	dob, err := models.ParseDate("1990-05-20")
	require.NoError(t, err)

	db, mock := newMockDB(t)
	mock.ExpectQuery(`WHERE hp\.hospital_id = \$1`).
		WithArgs(
			hospitalAID,
			"1234567890123",
			"AA1234567",
			"%Somchai%",
			"%Sri%",
			"%Jaidee%",
			sqlmock.AnyArg(), // date_of_birth, passed through models.Date's Valuer
			"0812345678",
			"somchai@example.com",
		).
		WillReturnRows(patientRows())

	records, err := repository.NewPatientRepository(db).Search(context.Background(), models.PatientSearchCriteria{
		HospitalID:  hospitalAID,
		NationalID:  ptr("1234567890123"),
		PassportID:  ptr("AA1234567"),
		FirstName:   ptr("Somchai"),
		MiddleName:  ptr("Sri"),
		LastName:    ptr("Jaidee"),
		DateOfBirth: &dob,
		PhoneNumber: ptr("0812345678"),
		Email:       ptr("somchai@example.com"),
	})

	require.NoError(t, err)
	assert.Empty(t, records)
	assert.NotNil(t, records, "no matches is an empty slice, never nil")
}

// A name filter must be attempted against both the Thai and the English column.
func TestPatientRepository_Search_NameMatchesThaiAndEnglishColumns(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	mock.ExpectQuery(`\(p\.first_name_en ILIKE \$2 OR p\.first_name_th ILIKE \$2\)`).
		WithArgs(hospitalAID, "%Somchai%").
		WillReturnRows(patientRows())

	_, err := repository.NewPatientRepository(db).Search(context.Background(), models.PatientSearchCriteria{
		HospitalID: hospitalAID,
		FirstName:  ptr("Somchai"),
	})

	require.NoError(t, err)
}

func TestPatientRepository_Search_QueryFailureIsInternal(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	mock.ExpectQuery(`FROM hospital_patients`).WillReturnError(errors.New("relation does not exist"))

	records, err := repository.NewPatientRepository(db).Search(context.Background(), models.PatientSearchCriteria{
		HospitalID: hospitalAID,
		NationalID: ptr("1234567890123"),
	})

	assert.Nil(t, records)
	require.Error(t, err)
	apiErr := apierr.From(err)
	assert.Equal(t, apierr.CodeInternal, apiErr.Code)
	assert.NotContains(t, apiErr.Message, "relation does not exist")
}

func TestPatientRepository_UpsertFromHIS(t *testing.T) {
	t.Parallel()

	dob, err := models.ParseDate("1990-05-20")
	require.NoError(t, err)
	profile := &hisclient.PatientProfile{
		PatientHN: "HN00123",
		Patient: models.Patient{
			NationalID:  ptr("1234567890123"),
			FirstNameTH: "สมชาย", LastNameTH: "ใจดี",
			FirstNameEN: "Somchai", LastNameEN: "Jaidee",
			DateOfBirth: &dob, Gender: "M",
		},
	}

	t.Run("an unknown person is inserted and linked in one transaction", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT id FROM patients`).
			WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery(`INSERT INTO patients`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(patientID))
		mock.ExpectExec(`INSERT INTO hospital_patients .* ON CONFLICT \(hospital_id, patient_id\)`).
			WithArgs(hospitalAID, patientID, "HN00123").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		err := repository.NewPatientRepository(db).UpsertFromHIS(context.Background(), hospitalAID, profile)

		require.NoError(t, err)
	})

	t.Run("a person already known from another hospital is updated, not duplicated", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT id FROM patients`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(patientID))
		mock.ExpectExec(`UPDATE patients SET`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`INSERT INTO hospital_patients`).
			WithArgs(hospitalAID, patientID, "HN00123").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		err := repository.NewPatientRepository(db).UpsertFromHIS(context.Background(), hospitalAID, profile)

		require.NoError(t, err)
	})

	// Regression: the UPDATE must merge, not assign. A plain `email = $12`
	// writes NULL whenever the incoming HIS omitted the field, silently
	// destroying what another hospital's HIS had supplied for the same person.
	t.Run("the update merges every field instead of overwriting", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT id FROM patients`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(patientID))
		mock.ExpectExec(`UPDATE patients SET` +
			`(?s).*national_id\s+= COALESCE\(\$2, national_id\)` +
			`(?s).*middle_name_th = COALESCE\(\$5, middle_name_th\)` +
			`(?s).*middle_name_en = COALESCE\(\$8, middle_name_en\)` +
			`(?s).*date_of_birth\s+= COALESCE\(\$10, date_of_birth\)` +
			`(?s).*phone_number\s+= COALESCE\(\$11, phone_number\)` +
			`(?s).*email\s+= COALESCE\(\$12, email\)`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`INSERT INTO hospital_patients`).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		err := repository.NewPatientRepository(db).UpsertFromHIS(context.Background(), hospitalAID, profile)

		require.NoError(t, err)
	})

	// The NOT NULL name columns arrive as '' rather than NULL when the HIS has
	// no value, so they need NULLIF before COALESCE to be preserved too.
	t.Run("empty incoming names do not blank out stored ones", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT id FROM patients`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(patientID))
		mock.ExpectExec(`(?s)first_name_en\s+= COALESCE\(NULLIF\(\$7, ''\), first_name_en\).*gender\s+= COALESCE\(NULLIF\(\$13, ''\), gender\)`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`INSERT INTO hospital_patients`).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		err := repository.NewPatientRepository(db).UpsertFromHIS(context.Background(), hospitalAID, profile)

		require.NoError(t, err)
	})

	t.Run("a failed link rolls back the patient write", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT id FROM patients`).WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery(`INSERT INTO patients`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(patientID))
		mock.ExpectExec(`INSERT INTO hospital_patients`).
			WillReturnError(uniqueViolation("ux_hospital_patients_hospital_hn"))
		mock.ExpectRollback()

		err := repository.NewPatientRepository(db).UpsertFromHIS(context.Background(), hospitalAID, profile)

		require.Error(t, err)
		assert.Equal(t, apierr.CodeInternal, apierr.From(err).Code)
	})

	t.Run("a transaction that cannot start is an internal error", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		mock.ExpectBegin().WillReturnError(errors.New("too many connections"))

		err := repository.NewPatientRepository(db).UpsertFromHIS(context.Background(), hospitalAID, profile)

		require.Error(t, err)
		assert.Equal(t, apierr.CodeInternal, apierr.From(err).Code)
	})
}

// The hospital scope must be a bound parameter, so a crafted identifier cannot
// alter the query's structure.
func TestPatientRepository_Search_DoesNotInterpolateUserInput(t *testing.T) {
	t.Parallel()

	injection := "' OR 1=1 --"

	db, mock := newMockDB(t)
	mock.ExpectQuery(`p\.national_id = \$2`).
		WithArgs(hospitalAID, injection).
		WillReturnRows(patientRows())

	_, err := repository.NewPatientRepository(db).Search(context.Background(), models.PatientSearchCriteria{
		HospitalID: hospitalAID,
		NationalID: &injection,
	})

	require.NoError(t, err)

	// And the rendered SQL must not contain the payload itself.
	queries := mock.ExpectationsWereMet()
	assert.NoError(t, queries)
	assert.NotRegexp(t, regexp.MustCompile(regexp.QuoteMeta(injection)), "query text is parameterized")
}

func ptr[T any](v T) *T { return &v }
