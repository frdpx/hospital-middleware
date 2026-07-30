package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/bambam/hospital-middleware/internal/apierr"
	"github.com/bambam/hospital-middleware/internal/hisclient"
	"github.com/bambam/hospital-middleware/internal/models"
)

// searchResultLimit caps a single search. Patient data is sensitive; an
// over-broad filter should return a usable page, not the hospital's roster.
const searchResultLimit = 100

// DB is a Querier that can also start transactions. *sql.DB satisfies it, and
// so does the *sql.DB that sqlmock hands to tests.
type DB interface {
	Querier
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

type PatientRepository struct {
	db DB
}

func NewPatientRepository(db DB) *PatientRepository {
	return &PatientRepository{db: db}
}

// patientColumns is the projection shared by every patient read. `p` is the
// patients alias and `hp` the hospital_patients alias.
const patientColumns = `
	p.id, p.national_id, p.passport_id,
	p.first_name_th, p.middle_name_th, p.last_name_th,
	p.first_name_en, p.middle_name_en, p.last_name_en,
	p.date_of_birth, p.phone_number, p.email, p.gender,
	p.created_at, p.updated_at,
	hp.patient_hn, hp.synced_at`

// Search returns patients visible to criteria.HospitalID.
//
// The hospital filter is the first WHERE clause and is never optional: the
// query starts from hospital_patients, so a patient row that exists only for
// another hospital is not reachable by this statement at all.
func (r *PatientRepository) Search(ctx context.Context, criteria models.PatientSearchCriteria) ([]models.PatientRecord, error) {
	where := newConditions()
	where.add("hp.hospital_id = %s", criteria.HospitalID)

	if criteria.NationalID != nil {
		where.add("p.national_id = %s", *criteria.NationalID)
	}
	if criteria.PassportID != nil {
		where.add("p.passport_id = %s", *criteria.PassportID)
	}
	// A search sends one `first_name`, but we store Thai and English forms
	// separately, so either may match. Same for middle and last name.
	if criteria.FirstName != nil {
		where.addNameMatch("first_name", *criteria.FirstName)
	}
	if criteria.MiddleName != nil {
		where.addNameMatch("middle_name", *criteria.MiddleName)
	}
	if criteria.LastName != nil {
		where.addNameMatch("last_name", *criteria.LastName)
	}
	if criteria.DateOfBirth != nil {
		where.add("p.date_of_birth = %s", *criteria.DateOfBirth)
	}
	if criteria.PhoneNumber != nil {
		where.add("p.phone_number = %s", *criteria.PhoneNumber)
	}
	if criteria.Email != nil {
		where.add("lower(p.email) = lower(%s)", *criteria.Email)
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM hospital_patients hp
		JOIN patients p ON p.id = hp.patient_id
		WHERE %s
		ORDER BY p.last_name_en, p.first_name_en, p.id
		LIMIT %d`, patientColumns, where.sql(), searchResultLimit)

	rows, err := r.db.QueryContext(ctx, query, where.args()...)
	if err != nil {
		return nil, apierr.Internal().WithCause(err)
	}
	defer func() { _ = rows.Close() }()

	records := make([]models.PatientRecord, 0)
	for rows.Next() {
		record, err := scanPatientRecord(rows)
		if err != nil {
			return nil, apierr.Internal().WithCause(err)
		}
		records = append(records, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, apierr.Internal().WithCause(err)
	}
	return records, nil
}

// UpsertFromHIS stores a patient fetched from a hospital's HIS and links it to
// that hospital.
//
// Both writes happen in one transaction: a patients row with no matching
// hospital_patients row would be invisible to every search, which is worse
// than not having stored it at all.
func (r *PatientRepository) UpsertFromHIS(ctx context.Context, hospitalID uuid.UUID, profile *hisclient.PatientProfile) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return apierr.Internal().WithCause(err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once Commit succeeded

	patientID, err := upsertPatient(ctx, tx, profile.Patient)
	if err != nil {
		return err
	}
	if err := linkPatientToHospital(ctx, tx, hospitalID, patientID, profile.PatientHN); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return apierr.Internal().WithCause(err)
	}
	return nil
}

// patientIdentifierConstraints are the two partial unique indexes that keep one
// canonical row per government identifier.
var patientIdentifierConstraints = []string{"ux_patients_national_id", "ux_patients_passport_id"}

func isIdentifierConflict(err error) bool {
	for _, constraint := range patientIdentifierConstraints {
		if isUniqueViolation(err, constraint) {
			return true
		}
	}
	return false
}

// upsertPatient finds the canonical person by either identifier and updates
// them, or inserts a new row.
//
// This is a find-then-write rather than a single ON CONFLICT: patients has two
// *partial* unique indexes (national_id, passport_id) and a record may collide
// on either, which a single conflict target cannot express.
func upsertPatient(ctx context.Context, tx Querier, patient models.Patient) (uuid.UUID, error) {
	existingID, found, err := findPatientByIdentifier(ctx, tx, patient)
	if err != nil {
		return uuid.Nil, err
	}
	if found {
		return existingID, updatePatient(ctx, tx, existingID, patient)
	}

	// In Postgres a failed statement poisons the whole transaction, so the
	// INSERT is fenced by a savepoint we can roll back to and carry on from.
	if _, err := tx.ExecContext(ctx, "SAVEPOINT before_patient_insert"); err != nil {
		return uuid.Nil, apierr.Internal().WithCause(err)
	}

	id, err := insertPatient(ctx, tx, patient)
	if !isIdentifierConflict(err) {
		return id, err
	}

	// We lost a race: between our SELECT and our INSERT, a concurrent search
	// for the same patient created the row. That is expected under load, not a
	// failure — rewind to the savepoint and merge into the winner instead.
	if _, rollbackErr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT before_patient_insert"); rollbackErr != nil {
		return uuid.Nil, apierr.Internal().WithCause(rollbackErr)
	}

	existingID, found, findErr := findPatientByIdentifier(ctx, tx, patient)
	if findErr != nil {
		return uuid.Nil, findErr
	}
	if !found {
		// The conflict was on an identifier we did not search by, i.e. this
		// person's national_id already belongs to a different patient row.
		// Two upstream systems disagree; a human has to resolve it.
		return uuid.Nil, apierr.IdentifierConflict().WithCause(err)
	}
	return existingID, updatePatient(ctx, tx, existingID, patient)
}

func findPatientByIdentifier(ctx context.Context, tx Querier, patient models.Patient) (uuid.UUID, bool, error) {
	const query = `
		SELECT id FROM patients
		WHERE ($1::text IS NOT NULL AND national_id = $1)
		   OR ($2::text IS NOT NULL AND passport_id = $2)
		LIMIT 1`

	var id uuid.UUID
	err := tx.QueryRowContext(ctx, query, patient.NationalID, patient.PassportID).Scan(&id)
	switch {
	case err == nil:
		return id, true, nil
	case errors.Is(err, sql.ErrNoRows):
		return uuid.Nil, false, nil
	default:
		return uuid.Nil, false, apierr.Internal().WithCause(err)
	}
}

func insertPatient(ctx context.Context, tx Querier, patient models.Patient) (uuid.UUID, error) {
	const query = `
		INSERT INTO patients (
			national_id, passport_id,
			first_name_th, middle_name_th, last_name_th,
			first_name_en, middle_name_en, last_name_en,
			date_of_birth, phone_number, email, gender
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id`

	var id uuid.UUID
	err := tx.QueryRowContext(ctx, query,
		patient.NationalID, patient.PassportID,
		patient.FirstNameTH, patient.MiddleNameTH, patient.LastNameTH,
		patient.FirstNameEN, patient.MiddleNameEN, patient.LastNameEN,
		patient.DateOfBirth, patient.PhoneNumber, patient.Email, patient.Gender,
	).Scan(&id)
	switch {
	case isIdentifierConflict(err):
		// Returned raw so the caller can tell a lost race from a real failure.
		return uuid.Nil, err
	case err != nil:
		return uuid.Nil, apierr.Internal().WithCause(err)
	}
	return id, nil
}

// updatePatient refreshes demographics from the HIS.
//
// Every field is merged, never overwritten: a value the incoming HIS did not
// report (NULL, or an empty string for the NOT NULL columns) leaves the stored
// one intact.
//
// This matters because `patients` is shared across hospitals. Hospital B's HIS
// may hold a thinner record of the same person than Hospital A's did, and a
// plain assignment would let a search from B silently erase what A supplied —
// destroying exactly the cross-hospital identity this table exists to hold.
//
// The trade-off: a field can never be *cleared* through this path. For a
// middleware that caches upstream data, preserving a stale value is strictly
// safer than destroying a good one, and a genuine deletion is an operator
// action rather than a side effect of somebody running a search.
func updatePatient(ctx context.Context, tx Querier, id uuid.UUID, patient models.Patient) error {
	const query = `
		UPDATE patients SET
			national_id    = COALESCE($2, national_id),
			passport_id    = COALESCE($3, passport_id),
			first_name_th  = COALESCE(NULLIF($4, ''), first_name_th),
			middle_name_th = COALESCE($5, middle_name_th),
			last_name_th   = COALESCE(NULLIF($6, ''), last_name_th),
			first_name_en  = COALESCE(NULLIF($7, ''), first_name_en),
			middle_name_en = COALESCE($8, middle_name_en),
			last_name_en   = COALESCE(NULLIF($9, ''), last_name_en),
			date_of_birth  = COALESCE($10, date_of_birth),
			phone_number   = COALESCE($11, phone_number),
			email          = COALESCE($12, email),
			gender         = COALESCE(NULLIF($13, ''), gender),
			updated_at     = now()
		WHERE id = $1`

	_, err := tx.ExecContext(ctx, query, id,
		patient.NationalID, patient.PassportID,
		patient.FirstNameTH, patient.MiddleNameTH, patient.LastNameTH,
		patient.FirstNameEN, patient.MiddleNameEN, patient.LastNameEN,
		patient.DateOfBirth, patient.PhoneNumber, patient.Email, patient.Gender,
	)
	switch {
	case isIdentifierConflict(err):
		// The incoming record carries an identifier that already belongs to a
		// different canonical patient — two upstream systems disagree about who
		// this person is. A 409 tells an operator that; a 500 tells them
		// nothing.
		return apierr.IdentifierConflict().WithCause(err)
	case err != nil:
		return apierr.Internal().WithCause(err)
	}
	return nil
}

// linkPatientToHospital records that this hospital knows the patient, under
// the HN its own HIS assigned.
func linkPatientToHospital(ctx context.Context, tx Querier, hospitalID, patientID uuid.UUID, patientHN string) error {
	const query = `
		INSERT INTO hospital_patients (hospital_id, patient_id, patient_hn)
		VALUES ($1, $2, $3)
		ON CONFLICT (hospital_id, patient_id)
		DO UPDATE SET patient_hn = EXCLUDED.patient_hn, synced_at = now()`

	if _, err := tx.ExecContext(ctx, query, hospitalID, patientID, patientHN); err != nil {
		return apierr.Internal().WithCause(err)
	}
	return nil
}

func scanPatientRecord(rows *sql.Rows) (*models.PatientRecord, error) {
	var r models.PatientRecord
	err := rows.Scan(
		&r.ID, &r.NationalID, &r.PassportID,
		&r.FirstNameTH, &r.MiddleNameTH, &r.LastNameTH,
		&r.FirstNameEN, &r.MiddleNameEN, &r.LastNameEN,
		&r.DateOfBirth, &r.PhoneNumber, &r.Email, &r.Gender,
		&r.CreatedAt, &r.UpdatedAt,
		&r.PatientHN, &r.SyncedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// conditions accumulates WHERE fragments and their bind parameters, so filters
// are composed without ever interpolating a user value into SQL text.
type conditions struct {
	clauses []string
	params  []any
}

func newConditions() *conditions {
	return &conditions{}
}

// add appends a clause. The template must contain %s exactly where the bind
// placeholder belongs — the value itself is always bound, never formatted in.
func (c *conditions) add(template string, value any) {
	c.params = append(c.params, value)
	c.clauses = append(c.clauses, fmt.Sprintf(template, c.placeholder()))
}

// addNameMatch matches a single supplied name against both the Thai and the
// English column, case-insensitively and as a substring, because staff rarely
// know which script a record was entered in.
func (c *conditions) addNameMatch(column, value string) {
	c.params = append(c.params, "%"+value+"%")
	placeholder := c.placeholder()
	c.clauses = append(c.clauses, fmt.Sprintf(
		"(p.%s_en ILIKE %s OR p.%s_th ILIKE %s)",
		column, placeholder, column, placeholder,
	))
}

// placeholder returns the positional marker for the most recently added param.
func (c *conditions) placeholder() string {
	return fmt.Sprintf("$%d", len(c.params))
}

func (c *conditions) sql() string {
	return strings.Join(c.clauses, "\n\t\t  AND ")
}

func (c *conditions) args() []any {
	return c.params
}
