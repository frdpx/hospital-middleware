package hisclient

import (
	"context"
	"errors"

	"github.com/frdpx/hospital-middleware/internal/models"
)

var ErrPatientNotFound = errors.New("hisclient: patient not found")

var ErrUnavailable = errors.New("hisclient: his unavailable")

type PatientProfile struct {
	Patient   models.Patient
	PatientHN string
}

type HISClient interface {
	FetchPatientByID(ctx context.Context, id string) (*PatientProfile, error)
}

type Factory interface {
	ClientFor(hospital models.Hospital) (HISClient, error)
}
