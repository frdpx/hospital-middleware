package models

import (
	"time"

	"github.com/google/uuid"
)

// Gender values as returned by the HIS. Stored as-is; empty means the HIS did
// not provide one.
const (
	GenderMale   = "M"
	GenderFemale = "F"
)

// Patient is the canonical record of a person, independent of any hospital.
// It is keyed by the identifiers that are globally unique across hospitals
// (national_id / passport_id) so the same human is not duplicated when they
// appear in a second hospital's HIS.
type Patient struct {
	ID           uuid.UUID `json:"id"`
	NationalID   *string   `json:"national_id"`
	PassportID   *string   `json:"passport_id"`
	FirstNameTH  string    `json:"first_name_th"`
	MiddleNameTH *string   `json:"middle_name_th"`
	LastNameTH   string    `json:"last_name_th"`
	FirstNameEN  string    `json:"first_name_en"`
	MiddleNameEN *string   `json:"middle_name_en"`
	LastNameEN   string    `json:"last_name_en"`
	DateOfBirth  *Date     `json:"date_of_birth"`
	PhoneNumber  *string   `json:"phone_number"`
	Email        *string   `json:"email"`
	Gender       string    `json:"gender"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// HospitalPatient links a canonical Patient to one hospital, carrying that
// hospital's local hospital number. It is the scoping row: every patient
// search joins through it and filters by hospital_id.
type HospitalPatient struct {
	ID         uuid.UUID `json:"id"`
	HospitalID uuid.UUID `json:"hospital_id"`
	PatientID  uuid.UUID `json:"patient_id"`
	PatientHN  string    `json:"patient_hn"`
	SyncedAt   time.Time `json:"synced_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// PatientRecord is the joined view a search returns: the person, plus the HN
// the requesting staff member's hospital knows them by.
type PatientRecord struct {
	Patient
	PatientHN string    `json:"patient_hn"`
	SyncedAt  time.Time `json:"synced_at"`
}

// PatientSearchCriteria holds the optional filters of /patient/search. Every
// field is a pointer so "absent" is distinguishable from "empty string".
//
// HospitalID is not user input — it is injected by the service from the JWT,
// which is what makes cross-hospital access impossible at the query level.
type PatientSearchCriteria struct {
	HospitalID  uuid.UUID
	NationalID  *string
	PassportID  *string
	FirstName   *string
	MiddleName  *string
	LastName    *string
	DateOfBirth *Date
	PhoneNumber *string
	Email       *string
}

// IsEmpty reports whether no filter at all was supplied. An empty search would
// dump the hospital's entire patient list, so callers reject it.
func (c PatientSearchCriteria) IsEmpty() bool {
	return c.NationalID == nil &&
		c.PassportID == nil &&
		c.FirstName == nil &&
		c.MiddleName == nil &&
		c.LastName == nil &&
		c.DateOfBirth == nil &&
		c.PhoneNumber == nil &&
		c.Email == nil
}

// HasIdentifier reports whether the search included a globally unique
// identifier. Only these searches can fall back to the HIS, because the HIS
// lookup endpoint takes a national_id or passport_id and nothing else.
func (c PatientSearchCriteria) HasIdentifier() bool {
	return c.NationalID != nil || c.PassportID != nil
}

// Identifier returns the value to send to the HIS, preferring national_id.
func (c PatientSearchCriteria) Identifier() string {
	if c.NationalID != nil {
		return *c.NationalID
	}
	if c.PassportID != nil {
		return *c.PassportID
	}
	return ""
}
