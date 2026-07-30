package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"unicode/utf8"

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

	// Nothing stored locally. Name/phone/email searches end here: the HIS has
	// no endpoint to search by those, so an empty result is the honest answer.
	if !criteria.HasIdentifier() {
		return records, nil
	}

	// The full filter matched nothing, but that is not the same as "we have
	// never heard of this patient". If the identifier alone is already on file
	// for this hospital, the miss came from the *other* criteria — and calling
	// the HIS would return the same patient, who would still fail those
	// criteria. Without this check every such request costs one upstream call
	// and one write transaction, forever, for a guaranteed 404.
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

// minNameFilterLength is counted in runes, not bytes: a Thai character is
// three bytes, so a byte-based check would reject "สม" while accepting "ab".
const minNameFilterLength = 2

// validateNameFilters rejects name fragments too short to be a search.
//
// Name matching is a substring match, so a one-character filter matches most of
// the hospital's roster and turns the endpoint into a bulk export of patient
// records — complete with national ids — up to the result limit. Requiring two
// characters costs legitimate users nothing and removes the single-character
// enumeration case entirely.
func validateNameFilters(criteria models.PatientSearchCriteria) error {
	filters := map[string]*string{
		"first_name":  criteria.FirstName,
		"middle_name": criteria.MiddleName,
		"last_name":   criteria.LastName,
	}
	// Iterated in a fixed order so the reported field is deterministic.
	for _, field := range []string{"first_name", "middle_name", "last_name"} {
		value := filters[field]
		if value != nil && utf8.RuneCountInString(*value) < minNameFilterLength {
			return apierr.Validation(fmt.Sprintf(
				"%s must be at least %d characters to search by name", field, minNameFilterLength))
		}
	}
	return nil
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
