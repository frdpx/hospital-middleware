package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/bambam/hospital-middleware/internal/apierr"
	"github.com/bambam/hospital-middleware/internal/models"
)

// staffUsernameConstraint is the composite unique index from migration 000001.
// It encodes the rule that a username is unique per hospital, not globally.
const staffUsernameConstraint = "ux_staff_hospital_username"

type StaffRepository struct {
	db Querier
}

func NewStaffRepository(db Querier) *StaffRepository {
	return &StaffRepository{db: db}
}

// Create inserts a staff row. A username already taken at that hospital
// surfaces as apierr.UsernameTaken; the same username at another hospital is
// allowed and inserts normally.
func (r *StaffRepository) Create(ctx context.Context, staff *models.Staff) (*models.Staff, error) {
	const query = `
		INSERT INTO staff (hospital_id, username, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at`

	created := *staff
	err := r.db.QueryRowContext(ctx, query, staff.HospitalID, staff.Username, staff.PasswordHash).
		Scan(&created.ID, &created.CreatedAt, &created.UpdatedAt)

	switch {
	case isUniqueViolation(err, staffUsernameConstraint):
		return nil, apierr.UsernameTaken().WithCause(err)
	case err != nil:
		return nil, apierr.Internal().WithCause(err)
	}
	return &created, nil
}

// FindByHospitalAndUsername looks a staff member up by the (hospital, username)
// pair. Never by username alone — usernames are only unique within a hospital,
// so a username-only lookup could return another hospital's account.
//
// A missing row returns (nil, nil) rather than an error: the caller must still
// spend the same time verifying a password so that a wrong username and a
// wrong password are indistinguishable to an attacker.
func (r *StaffRepository) FindByHospitalAndUsername(ctx context.Context, hospitalID uuid.UUID, username string) (*models.Staff, error) {
	const query = `
		SELECT id, hospital_id, username, password_hash, created_at, updated_at
		FROM staff
		WHERE hospital_id = $1 AND lower(username) = lower($2)`

	var s models.Staff
	err := r.db.QueryRowContext(ctx, query, hospitalID, username).Scan(
		&s.ID,
		&s.HospitalID,
		&s.Username,
		&s.PasswordHash,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apierr.Internal().WithCause(err)
	}
	return &s, nil
}
