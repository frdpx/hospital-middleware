package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/bambam/hospital-middleware/internal/apierr"
	"github.com/bambam/hospital-middleware/internal/hisclient"
	"github.com/bambam/hospital-middleware/internal/models"
)

// PatientRepository is the slice of patient storage the service needs.
type PatientRepository interface {
	Search(ctx context.Context, criteria models.PatientSearchCriteria) ([]models.PatientRecord, error)
	UpsertFromHIS(ctx context.Context, hospitalID uuid.UUID, profile *hisclient.PatientProfile) error
}

type PatientService struct {
	hospitals HospitalRepository
	patients  PatientRepository
	his       hisclient.Factory
	logger    *slog.Logger
}

func NewPatientService(
	hospitals HospitalRepository,
	patients PatientRepository,
	his hisclient.Factory,
	logger *slog.Logger,
) *PatientService {
	return &PatientService{hospitals: hospitals, patients: patients, his: his, logger: logger}
}

// Search finds patients within one hospital.
//
// criteria.HospitalID comes from the caller's JWT and is applied inside the
// SQL query, so there is no code path — including the HIS fallback — that can
// return a patient belonging to a different hospital.
//
// The flow is: local first, then HIS. Falling back to the HIS is only possible
// for national_id/passport_id searches, because that identifier is the only
// lookup key the HIS exposes.
func (s *PatientService) Search(ctx context.Context, criteria models.PatientSearchCriteria) ([]models.PatientRecord, error) {
	if criteria.HospitalID == uuid.Nil {
		// Defensive: a nil scope would drop the hospital filter's meaning.
		return nil, apierr.Unauthorized("request has no hospital scope")
	}
	if criteria.IsEmpty() {
		return nil, apierr.Validation("at least one search field is required")
	}

	records, err := s.patients.Search(ctx, criteria)
	if err != nil {
		return nil, err
	}
	if len(records) > 0 {
		return records, nil
	}

	// Nothing stored locally. Name/phone/email searches end here: the HIS has
	// no endpoint to search by those, so an empty result is the honest answer.
	if !criteria.HasIdentifier() {
		return records, nil
	}

	if err := s.syncFromHIS(ctx, criteria); err != nil {
		return nil, err
	}

	// Re-run the same scoped query rather than returning the HIS payload
	// directly: this keeps the hospital filter and every other supplied
	// criterion authoritative, so a patient whose name does not match the
	// request is not returned just because their id did.
	records, err = s.patients.Search(ctx, criteria)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, apierr.PatientNotFound()
	}
	return records, nil
}

// syncFromHIS fetches the patient from the hospital's own HIS and stores them.
func (s *PatientService) syncFromHIS(ctx context.Context, criteria models.PatientSearchCriteria) error {
	hospital, err := s.hospitals.FindByID(ctx, criteria.HospitalID)
	if err != nil {
		return err
	}

	client, err := s.his.ClientFor(*hospital)
	if err != nil {
		s.logger.WarnContext(ctx, "no usable HIS adapter",
			"hospital_id", hospital.ID, "hospital_code", hospital.Code, "error", err)
		return apierr.HISUnavailable().WithCause(err)
	}

	profile, err := client.FetchPatientByID(ctx, criteria.Identifier())
	switch {
	case errors.Is(err, hisclient.ErrPatientNotFound):
		return apierr.PatientNotFound()
	case err != nil:
		s.logger.ErrorContext(ctx, "HIS lookup failed",
			"hospital_id", hospital.ID, "hospital_code", hospital.Code, "error", err)
		return apierr.HISUnavailable().WithCause(err)
	}

	if err := s.patients.UpsertFromHIS(ctx, hospital.ID, profile); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "patient synced from HIS",
		"hospital_id", hospital.ID, "patient_hn", profile.PatientHN)
	return nil
}
