package hisclient

import (
	"context"
	"sync"

	"github.com/bambam/hospital-middleware/internal/models"
)

// FakeClient is an in-memory HISClient for tests. It lives in the production
// package (not a _test.go file) on purpose, so that the service and handler
// test suites can reuse it instead of each rolling their own stub.
type FakeClient struct {
	mu sync.Mutex

	// Profiles is keyed by identifier; register the same profile under both
	// its national id and its passport id to mimic a real HIS.
	Profiles map[string]*PatientProfile
	// Err, when set, is returned by every call — used to exercise the
	// HIS_UNAVAILABLE path.
	Err error
	// Calls records every identifier requested, so tests can assert that the
	// HIS was (or was not) consulted.
	Calls []string
}

func NewFakeClient() *FakeClient {
	return &FakeClient{Profiles: map[string]*PatientProfile{}}
}

// Add registers a profile under each of its non-nil identifiers.
func (f *FakeClient) Add(profile *PatientProfile) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if profile.Patient.NationalID != nil {
		f.Profiles[*profile.Patient.NationalID] = profile
	}
	if profile.Patient.PassportID != nil {
		f.Profiles[*profile.Patient.PassportID] = profile
	}
}

func (f *FakeClient) FetchPatientByID(_ context.Context, id string) (*PatientProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Calls = append(f.Calls, id)
	if f.Err != nil {
		return nil, f.Err
	}
	profile, ok := f.Profiles[id]
	if !ok {
		return nil, ErrPatientNotFound
	}
	return profile, nil
}

// CallCount reports how many times the HIS was queried.
func (f *FakeClient) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls)
}

// FakeFactory hands the same FakeClient to every hospital. Set Err to simulate
// a hospital whose HIS adapter cannot be resolved at all.
type FakeFactory struct {
	Client HISClient
	Err    error
}

func NewFakeFactory(client HISClient) *FakeFactory {
	return &FakeFactory{Client: client}
}

func (f *FakeFactory) ClientFor(_ models.Hospital) (HISClient, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Client, nil
}
