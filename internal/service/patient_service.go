package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/frdpx/hospital-middleware/internal/apierr"
	"github.com/frdpx/hospital-middleware/internal/hisclient"
	"github.com/frdpx/hospital-middleware/internal/models"
)

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

func (s *PatientService) Search(ctx context.Context, criteria models.PatientSearchCriteria) ([]models.PatientRecord, error) {
	if criteria.HospitalID == uuid.Nil {
		return nil, apierr.Unauthorized("request has no hospital scope")
	}
	if criteria.IsEmpty() {
		return nil, apierr.Validation("at least one search field is required")
	}
	if err := validateNameFilters(criteria); err != nil {
		return nil, err
	}

	records, err := s.patients.Search(ctx, criteria)
	if err != nil {
		return nil, err
	}
	if len(records) > 0 {
		return records, nil
	}

	if !criteria.HasIdentifier() {
		return records, nil
	}

	known, err := s.patients.Search(ctx, models.PatientSearchCriteria{
		HospitalID: criteria.HospitalID,
		NationalID: criteria.NationalID,
		PassportID: criteria.PassportID,
	})
	if err != nil {
		return nil, err
	}
	if len(known) > 0 {
		return nil, apierr.PatientNotFound()
	}

	if err := s.syncFromHIS(ctx, criteria); err != nil {
		return nil, err
	}

	records, err = s.patients.Search(ctx, criteria)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, apierr.PatientNotFound()
	}
	return records, nil
}

const minNameFilterLength = 2

func validateNameFilters(criteria models.PatientSearchCriteria) error {
	filters := map[string]*string{
		"first_name":  criteria.FirstName,
		"middle_name": criteria.MiddleName,
		"last_name":   criteria.LastName,
	}

	for _, field := range []string{"first_name", "middle_name", "last_name"} {
		value := filters[field]
		if value != nil && utf8.RuneCountInString(*value) < minNameFilterLength {
			return apierr.Validation(fmt.Sprintf(
				"%s must be at least %d characters to search by name", field, minNameFilterLength))
		}
	}
	return nil
}

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
