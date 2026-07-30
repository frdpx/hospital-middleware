package models

import (
	"time"

	"github.com/google/uuid"
)

const (
	GenderMale   = "M"
	GenderFemale = "F"
)

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

type HospitalPatient struct {
	ID         uuid.UUID `json:"id"`
	HospitalID uuid.UUID `json:"hospital_id"`
	PatientID  uuid.UUID `json:"patient_id"`
	PatientHN  string    `json:"patient_hn"`
	SyncedAt   time.Time `json:"synced_at"`
	CreatedAt  time.Time `json:"created_at"`
}

type PatientRecord struct {
	Patient
	PatientHN string    `json:"patient_hn"`
	SyncedAt  time.Time `json:"synced_at"`
}

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

func (c PatientSearchCriteria) HasIdentifier() bool {
	return c.NationalID != nil || c.PassportID != nil
}

func (c PatientSearchCriteria) Identifier() string {
	if c.NationalID != nil {
		return *c.NationalID
	}
	if c.PassportID != nil {
		return *c.PassportID
	}
	return ""
}
