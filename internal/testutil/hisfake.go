package testutil

import (
	"context"
	"sync"

	"github.com/frdpx/hospital-middleware/internal/hisclient"
	"github.com/frdpx/hospital-middleware/internal/models"
)

type FakeHIS struct {
	mu sync.Mutex

	Profiles map[string]*hisclient.PatientProfile

	Err error

	Calls []string
}

func NewFakeHIS() *FakeHIS {
	return &FakeHIS{Profiles: map[string]*hisclient.PatientProfile{}}
}

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

func (f *FakeHIS) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls)
}

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
