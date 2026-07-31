package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/frdpx/hospital-middleware/internal/apierr"
	"github.com/frdpx/hospital-middleware/internal/models"
)

const staffUsernameConstraint = "ux_staff_hospital_username"

type StaffRepository struct {
	db Querier
}

func NewStaffRepository(db Querier) *StaffRepository {
	return &StaffRepository{db: db}
}

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
