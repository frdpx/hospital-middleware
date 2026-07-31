package testutil

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/frdpx/hospital-middleware/internal/apierr"
	"github.com/frdpx/hospital-middleware/internal/hisclient"
	"github.com/frdpx/hospital-middleware/internal/models"
)

type HospitalRepo struct {
	mu        sync.Mutex
	hospitals []models.Hospital

	Err error
}

func NewHospitalRepo(hospitals ...models.Hospital) *HospitalRepo {
	return &HospitalRepo{hospitals: hospitals}
}

func (r *HospitalRepo) FindByCodeOrName(_ context.Context, identifier string) (*models.Hospital, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.Err != nil {
		return nil, r.Err
	}
	for i := range r.hospitals {
		h := r.hospitals[i]
		if strings.EqualFold(h.Code, identifier) || strings.EqualFold(h.Name, identifier) {
			return &h, nil
		}
	}
	return nil, apierr.HospitalNotFound()
}

func (r *HospitalRepo) FindByID(_ context.Context, id uuid.UUID) (*models.Hospital, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.Err != nil {
		return nil, r.Err
	}
	for i := range r.hospitals {
		if r.hospitals[i].ID == id {
			h := r.hospitals[i]
			return &h, nil
		}
	}
	return nil, apierr.HospitalNotFound()
}

type StaffRepo struct {
	mu    sync.Mutex
	staff []models.Staff

	CreateErr error
	FindErr   error
}

func NewStaffRepo(staff ...models.Staff) *StaffRepo {
	return &StaffRepo{staff: staff}
}

func (r *StaffRepo) Create(_ context.Context, staff *models.Staff) (*models.Staff, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.CreateErr != nil {
		return nil, r.CreateErr
	}
	for _, existing := range r.staff {
		if existing.HospitalID == staff.HospitalID && strings.EqualFold(existing.Username, staff.Username) {
			return nil, apierr.UsernameTaken()
		}
	}

	created := *staff
	created.ID = uuid.New()
	created.CreatedAt = time.Now().UTC()
	created.UpdatedAt = created.CreatedAt
	r.staff = append(r.staff, created)
	return &created, nil
}

func (r *StaffRepo) FindByHospitalAndUsername(_ context.Context, hospitalID uuid.UUID, username string) (*models.Staff, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.FindErr != nil {
		return nil, r.FindErr
	}
	for i := range r.staff {
		if r.staff[i].HospitalID == hospitalID && strings.EqualFold(r.staff[i].Username, username) {
			s := r.staff[i]
			return &s, nil
		}
	}

	return nil, nil
}

func (r *StaffRepo) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.staff)
}

type PatientRepo struct {
	mu       sync.Mutex
	patients []models.Patient
	links    []models.HospitalPatient

	SearchErr error
	UpsertErr error

	UpsertCalls int
}

func NewPatientRepo() *PatientRepo {
	return &PatientRepo{}
}

func (r *PatientRepo) Seed(hospitalID uuid.UUID, patientHN string, patient models.Patient) models.Patient {
	r.mu.Lock()
	defer r.mu.Unlock()

	if patient.ID == uuid.Nil {
		patient.ID = uuid.New()
	}
	r.patients = append(r.patients, patient)
	r.links = append(r.links, models.HospitalPatient{
		ID:         uuid.New(),
		HospitalID: hospitalID,
		PatientID:  patient.ID,
		PatientHN:  patientHN,
		SyncedAt:   time.Now().UTC(),
	})
	return patient
}

func (r *PatientRepo) Search(_ context.Context, criteria models.PatientSearchCriteria) ([]models.PatientRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.SearchErr != nil {
		return nil, r.SearchErr
	}

	records := make([]models.PatientRecord, 0)
	for _, link := range r.links {
		if link.HospitalID != criteria.HospitalID {
			continue
		}
		patient, ok := r.findPatient(link.PatientID)
		if !ok || !matches(patient, criteria) {
			continue
		}
		records = append(records, models.PatientRecord{
			Patient:   patient,
			PatientHN: link.PatientHN,
			SyncedAt:  link.SyncedAt,
		})
	}
	return records, nil
}

func (r *PatientRepo) UpsertFromHIS(_ context.Context, hospitalID uuid.UUID, profile *hisclient.PatientProfile) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.UpsertCalls++
	if r.UpsertErr != nil {
		return r.UpsertErr
	}

	patientID, found := r.findByIdentifier(profile.Patient)
	if found {
		for i := range r.patients {
			if r.patients[i].ID == patientID {
				r.patients[i] = mergePatient(r.patients[i], profile.Patient)
			}
		}
	} else {
		patient := profile.Patient
		patient.ID = uuid.New()
		patientID = patient.ID
		r.patients = append(r.patients, patient)
	}

	for i := range r.links {
		if r.links[i].HospitalID == hospitalID && r.links[i].PatientID == patientID {
			r.links[i].PatientHN = profile.PatientHN
			r.links[i].SyncedAt = time.Now().UTC()
			return nil
		}
	}
	r.links = append(r.links, models.HospitalPatient{
		ID:         uuid.New(),
		HospitalID: hospitalID,
		PatientID:  patientID,
		PatientHN:  profile.PatientHN,
		SyncedAt:   time.Now().UTC(),
	})
	return nil
}

func mergePatient(stored, incoming models.Patient) models.Patient {
	merged := stored

	merged.NationalID = firstNonNil(incoming.NationalID, stored.NationalID)
	merged.PassportID = firstNonNil(incoming.PassportID, stored.PassportID)
	merged.MiddleNameTH = firstNonNil(incoming.MiddleNameTH, stored.MiddleNameTH)
	merged.MiddleNameEN = firstNonNil(incoming.MiddleNameEN, stored.MiddleNameEN)
	merged.PhoneNumber = firstNonNil(incoming.PhoneNumber, stored.PhoneNumber)
	merged.Email = firstNonNil(incoming.Email, stored.Email)

	merged.FirstNameTH = firstNonEmpty(incoming.FirstNameTH, stored.FirstNameTH)
	merged.LastNameTH = firstNonEmpty(incoming.LastNameTH, stored.LastNameTH)
	merged.FirstNameEN = firstNonEmpty(incoming.FirstNameEN, stored.FirstNameEN)
	merged.LastNameEN = firstNonEmpty(incoming.LastNameEN, stored.LastNameEN)
	merged.Gender = firstNonEmpty(incoming.Gender, stored.Gender)

	if incoming.DateOfBirth != nil {
		merged.DateOfBirth = incoming.DateOfBirth
	}
	return merged
}

func firstNonNil(incoming, stored *string) *string {
	if incoming != nil {
		return incoming
	}
	return stored
}

func firstNonEmpty(incoming, stored string) string {
	if strings.TrimSpace(incoming) != "" {
		return incoming
	}
	return stored
}

func (r *PatientRepo) findPatient(id uuid.UUID) (models.Patient, bool) {
	for _, p := range r.patients {
		if p.ID == id {
			return p, true
		}
	}
	return models.Patient{}, false
}

func (r *PatientRepo) findByIdentifier(target models.Patient) (uuid.UUID, bool) {
	for _, p := range r.patients {
		if equalPtr(p.NationalID, target.NationalID) || equalPtr(p.PassportID, target.PassportID) {
			return p.ID, true
		}
	}
	return uuid.Nil, false
}

func matches(p models.Patient, c models.PatientSearchCriteria) bool {
	if c.NationalID != nil && !equalPtr(p.NationalID, c.NationalID) {
		return false
	}
	if c.PassportID != nil && !equalPtr(p.PassportID, c.PassportID) {
		return false
	}
	if c.FirstName != nil && !nameMatches(p.FirstNameEN, p.FirstNameTH, *c.FirstName) {
		return false
	}
	if c.MiddleName != nil && !nameMatches(deref(p.MiddleNameEN), deref(p.MiddleNameTH), *c.MiddleName) {
		return false
	}
	if c.LastName != nil && !nameMatches(p.LastNameEN, p.LastNameTH, *c.LastName) {
		return false
	}
	if c.DateOfBirth != nil {
		if p.DateOfBirth == nil || p.DateOfBirth.String() != c.DateOfBirth.String() {
			return false
		}
	}
	if c.PhoneNumber != nil && !equalPtr(p.PhoneNumber, c.PhoneNumber) {
		return false
	}
	if c.Email != nil {
		if p.Email == nil || !strings.EqualFold(*p.Email, *c.Email) {
			return false
		}
	}
	return true
}

func nameMatches(en, th, needle string) bool {
	needle = strings.ToLower(needle)
	return strings.Contains(strings.ToLower(en), needle) || strings.Contains(strings.ToLower(th), needle)
}

func equalPtr(a, b *string) bool {
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func Ptr[T any](v T) *T { return &v }
