package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/frdpx/hospital-middleware/internal/apierr"
	"github.com/frdpx/hospital-middleware/internal/hisclient"
	"github.com/frdpx/hospital-middleware/internal/models"
)

const searchResultLimit = 100

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

const patientColumns = `
	p.id, p.national_id, p.passport_id,
	p.first_name_th, p.middle_name_th, p.last_name_th,
	p.first_name_en, p.middle_name_en, p.last_name_en,
	p.date_of_birth, p.phone_number, p.email, p.gender,
	p.created_at, p.updated_at,
	hp.patient_hn, hp.synced_at`

func (r *PatientRepository) Search(ctx context.Context, criteria models.PatientSearchCriteria) ([]models.PatientRecord, error) {
	where := newConditions()
	where.add("hp.hospital_id = %s", criteria.HospitalID)

	if criteria.NationalID != nil {
		where.add("p.national_id = %s", *criteria.NationalID)
	}
	if criteria.PassportID != nil {
		where.add("p.passport_id = %s", *criteria.PassportID)
	}

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

func (r *PatientRepository) UpsertFromHIS(ctx context.Context, hospitalID uuid.UUID, profile *hisclient.PatientProfile) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return apierr.Internal().WithCause(err)
	}
	defer func() { _ = tx.Rollback() }()

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

var patientIdentifierConstraints = []string{"ux_patients_national_id", "ux_patients_passport_id"}

func isIdentifierConflict(err error) bool {
	for _, constraint := range patientIdentifierConstraints {
		if isUniqueViolation(err, constraint) {
			return true
		}
	}
	return false
}

func upsertPatient(ctx context.Context, tx Querier, patient models.Patient) (uuid.UUID, error) {
	existingID, found, err := findPatientByIdentifier(ctx, tx, patient)
	if err != nil {
		return uuid.Nil, err
	}
	if found {
		return existingID, updatePatient(ctx, tx, existingID, patient)
	}

	if _, err := tx.ExecContext(ctx, "SAVEPOINT before_patient_insert"); err != nil {
		return uuid.Nil, apierr.Internal().WithCause(err)
	}

	id, err := insertPatient(ctx, tx, patient)
	if !isIdentifierConflict(err) {
		return id, err
	}

	if _, rollbackErr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT before_patient_insert"); rollbackErr != nil {
		return uuid.Nil, apierr.Internal().WithCause(rollbackErr)
	}

	existingID, found, findErr := findPatientByIdentifier(ctx, tx, patient)
	if findErr != nil {
		return uuid.Nil, findErr
	}
	if !found {
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

		return uuid.Nil, err
	case err != nil:
		return uuid.Nil, apierr.Internal().WithCause(err)
	}
	return id, nil
}

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

		return apierr.IdentifierConflict().WithCause(err)
	case err != nil:
		return apierr.Internal().WithCause(err)
	}
	return nil
}

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

type conditions struct {
	clauses []string
	params  []any
}

func newConditions() *conditions {
	return &conditions{}
}

func (c *conditions) add(template string, value any) {
	c.params = append(c.params, value)
	c.clauses = append(c.clauses, fmt.Sprintf(template, c.placeholder()))
}

func (c *conditions) addNameMatch(column, value string) {
	c.params = append(c.params, "%"+value+"%")
	placeholder := c.placeholder()
	c.clauses = append(c.clauses, fmt.Sprintf(
		"(p.%s_en ILIKE %s OR p.%s_th ILIKE %s)",
		column, placeholder, column, placeholder,
	))
}

func (c *conditions) placeholder() string {
	return fmt.Sprintf("$%d", len(c.params))
}

func (c *conditions) sql() string {
	return strings.Join(c.clauses, "\n\t\t  AND ")
}

func (c *conditions) args() []any {
	return c.params
}
