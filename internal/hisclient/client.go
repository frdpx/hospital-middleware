// Package hisclient integrates with hospitals' Hospital Information Systems.
//
// Each hospital exposes its own HIS with its own payload shape. Everything in
// this package exists to hide that: callers depend on the HISClient interface
// and receive an already-normalized models.Patient, so adding "Hospital B"
// means adding one file here and one row in `hospitals` — no changes in the
// service, handler or database layers.
package hisclient

import (
	"context"
	"errors"

	"github.com/bambam/hospital-middleware/internal/models"
)

// ErrPatientNotFound is returned when the HIS answers successfully but holds
// no patient for the given identifier. It is distinct from a transport failure
// so the caller can render 404 instead of 502.
var ErrPatientNotFound = errors.New("hisclient: patient not found")

// ErrUnavailable wraps every transport-level failure: DNS, connection refused,
// timeout, 5xx, or an unparseable body. The caller maps it to HIS_UNAVAILABLE.
var ErrUnavailable = errors.New("hisclient: his unavailable")

// PatientProfile is the normalized result of a HIS lookup: the canonical
// person, plus the hospital-local HN that this particular HIS assigned them.
type PatientProfile struct {
	Patient   models.Patient
	PatientHN string
}

// HISClient fetches a patient from one hospital's HIS.
//
// The only lookup the assignment's HIS exposes is by a single identifier that
// may be either a national id or a passport id, so that is the entire
// interface. Name/phone/email search stays local by design.
type HISClient interface {
	// FetchPatientByID returns the patient the HIS knows under id, which may be
	// a national_id or a passport_id. It returns ErrPatientNotFound when the
	// HIS has no such patient, and an error wrapping ErrUnavailable otherwise.
	FetchPatientByID(ctx context.Context, id string) (*PatientProfile, error)
}

// Factory resolves the right HISClient for a hospital. Injecting a Factory
// (rather than a single client) is what lets one service instance talk to
// several hospitals' HIS systems.
type Factory interface {
	ClientFor(hospital models.Hospital) (HISClient, error)
}
