package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/bambam/hospital-middleware/internal/apierr"
	"github.com/bambam/hospital-middleware/internal/models"
)

const hospitalColumns = `id, code, name, his_adapter_type, his_base_url, created_at, updated_at`

type HospitalRepository struct {
	db Querier
}

func NewHospitalRepository(db Querier) *HospitalRepository {
	return &HospitalRepository{db: db}
}

// FindByCodeOrName resolves the free-text `hospital` field of /staff/create and
// /staff/login. Both the slug ("hospital-a") and the display name
// ("Hospital A") are accepted, case-insensitively, because the assignment does
// not say which form clients will send.
func (r *HospitalRepository) FindByCodeOrName(ctx context.Context, identifier string) (*models.Hospital, error) {
	const query = `
		SELECT ` + hospitalColumns + `
		FROM hospitals
		WHERE lower(code) = lower($1) OR lower(name) = lower($1)
		LIMIT 1`

	return scanHospital(r.db.QueryRowContext(ctx, query, identifier))
}

// FindByID loads the hospital referenced by a JWT, which is what tells the
// patient search which HIS adapter to use.
func (r *HospitalRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Hospital, error) {
	const query = `
		SELECT ` + hospitalColumns + `
		FROM hospitals
		WHERE id = $1`

	return scanHospital(r.db.QueryRowContext(ctx, query, id))
}

func scanHospital(row *sql.Row) (*models.Hospital, error) {
	var h models.Hospital
	err := row.Scan(
		&h.ID,
		&h.Code,
		&h.Name,
		&h.HISAdapterType,
		&h.HISBaseURL,
		&h.CreatedAt,
		&h.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.HospitalNotFound()
	}
	if err != nil {
		return nil, apierr.Internal().WithCause(err)
	}
	return &h, nil
}
