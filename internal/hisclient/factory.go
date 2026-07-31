package hisclient

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/frdpx/hospital-middleware/internal/models"
)

type DefaultFactory struct {
	httpClient *http.Client

	baseURLOverride string
}

func NewDefaultFactory(httpClient *http.Client, baseURLOverride string) *DefaultFactory {
	return &DefaultFactory{
		httpClient:      httpClient,
		baseURLOverride: strings.TrimSpace(baseURLOverride),
	}
}

func (f *DefaultFactory) ClientFor(hospital models.Hospital) (HISClient, error) {
	baseURL := f.baseURLOverride
	if baseURL == "" && hospital.HISBaseURL != nil {
		baseURL = strings.TrimSpace(*hospital.HISBaseURL)
	}
	if baseURL == "" {
		return nil, fmt.Errorf("%w: hospital %q has no HIS base URL configured", ErrUnavailable, hospital.Code)
	}

	switch hospital.HISAdapterType {
	case models.HISAdapterHospitalA:
		return NewHospitalAClient(baseURL, f.httpClient), nil
	default:
		return nil, fmt.Errorf("%w: no HIS adapter registered for type %q", ErrUnavailable, hospital.HISAdapterType)
	}
}
