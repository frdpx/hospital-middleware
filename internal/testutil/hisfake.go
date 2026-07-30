package testutil

import (
	"context"
	"sync"

	"github.com/bambam/hospital-middleware/internal/hisclient"
	"github.com/bambam/hospital-middleware/internal/models"
)

// FakeHIS is an in-memory hisclient.HISClient for tests.
//
// It lives here rather than in the hisclient package so that no test double is
// exported from — or compiled into — production code; cmd/api links hisclient,
// and a package's public API should not advertise fakes.
type FakeHIS struct {
	mu sync.Mutex

	// Profiles is keyed by identifier; register the same profile under both
	// its national id and its passport id to mimic a real HIS.
	Profiles map[string]*hisclient.PatientProfile
	// Err, when set, is returned by every call — used to exercise the
	// HIS_UNAVAILABLE path.
	Err error
	// Calls records every identifier requested, so tests can assert that the
	// HIS was (or was not) consulted.
	Calls []string
}

func NewFakeHIS() *FakeHIS {
	return &FakeHIS{Profiles: map[string]*hisclient.PatientProfile{}}
}

// Add registers a profile under each of its non-nil identifiers.
func (f *FakeHIS) Add(profile *hisclient.PatientProfile) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if profile.Patient.NationalID != nil {
		f.Profiles[*profile.Patient.NationalID] = profile
	}
	if profile.Patient.PassportID != nil {
		f.Profiles[*profile.Patient.PassportID] = profile
	}
}

func (f *FakeHIS) FetchPatientByID(_ context.Context, id string) (*hisclient.PatientProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Calls = append(f.Calls, id)
	if f.Err != nil {
		return nil, f.Err
	}
	profile, ok := f.Profiles[id]
	if !ok {
		return nil, hisclient.ErrPatientNotFound
	}
	return profile, nil
}

// CallCount reports how many times the HIS was queried.
func (f *FakeHIS) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls)
}

// FakeHISFactory hands the same FakeHIS to every hospital. Set Err to simulate
// a hospital whose HIS adapter cannot be resolved at all.
type FakeHISFactory struct {
	Client hisclient.HISClient
	Err    error
}

func NewFakeHISFactory(client hisclient.HISClient) *FakeHISFactory {
	return &FakeHISFactory{Client: client}
}

func (f *FakeHISFactory) ClientFor(_ models.Hospital) (hisclient.HISClient, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Client, nil
}
