package hisclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bambam/hospital-middleware/internal/models"
)

const maxHISResponseBytes = 1 << 20

type hospitalAResponse struct {
	FirstNameTH  string `json:"first_name_th"`
	MiddleNameTH string `json:"middle_name_th"`
	LastNameTH   string `json:"last_name_th"`
	FirstNameEN  string `json:"first_name_en"`
	MiddleNameEN string `json:"middle_name_en"`
	LastNameEN   string `json:"last_name_en"`
	DateOfBirth  string `json:"date_of_birth"`
	PatientHN    string `json:"patient_hn"`
	NationalID   string `json:"national_id"`
	PassportID   string `json:"passport_id"`
	PhoneNumber  string `json:"phone_number"`
	Email        string `json:"email"`
	Gender       string `json:"gender"`
}

type HospitalAClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewHospitalAClient(baseURL string, httpClient *http.Client) *HospitalAClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &HospitalAClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *HospitalAClient) FetchPatientByID(ctx context.Context, id string) (*PatientProfile, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("hisclient: empty patient identifier")
	}

	endpoint := fmt.Sprintf("%s/patient/search/%s", c.baseURL, url.PathEscape(id))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %w", ErrUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: call hospital A: %w", ErrUnavailable, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxHISResponseBytes))
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrPatientNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: hospital A returned status %d", ErrUnavailable, resp.StatusCode)
	}

	var payload hospitalAResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxHISResponseBytes)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: decode hospital A response: %w", ErrUnavailable, err)
	}

	profile, err := payload.toProfile()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return profile, nil
}

func (r hospitalAResponse) toProfile() (*PatientProfile, error) {
	nationalID := optional(r.NationalID)
	passportID := optional(r.PassportID)

	if nationalID == nil && passportID == nil {
		return nil, fmt.Errorf("response has neither national_id nor passport_id")
	}
	if strings.TrimSpace(r.PatientHN) == "" {
		return nil, fmt.Errorf("response has no patient_hn")
	}

	dob, err := parseHISDate(r.DateOfBirth)
	if err != nil {
		return nil, err
	}

	return &PatientProfile{
		PatientHN: strings.TrimSpace(r.PatientHN),
		Patient: models.Patient{
			NationalID:   nationalID,
			PassportID:   passportID,
			FirstNameTH:  strings.TrimSpace(r.FirstNameTH),
			MiddleNameTH: optional(r.MiddleNameTH),
			LastNameTH:   strings.TrimSpace(r.LastNameTH),
			FirstNameEN:  strings.TrimSpace(r.FirstNameEN),
			MiddleNameEN: optional(r.MiddleNameEN),
			LastNameEN:   strings.TrimSpace(r.LastNameEN),
			DateOfBirth:  dob,
			PhoneNumber:  optional(r.PhoneNumber),
			Email:        optional(r.Email),
			Gender:       normalizeGender(r.Gender),
		},
	}, nil
}

var hisDateLayouts = []string{
	models.DateLayout,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"02/01/2006",
}

func parseHISDate(raw string) (*models.Date, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	for _, layout := range hisDateLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			d := models.NewDate(t)
			return &d, nil
		}
	}
	return nil, fmt.Errorf("unrecognized date_of_birth %q", raw)
}

func normalizeGender(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "M", "MALE":
		return models.GenderMale
	case "F", "FEMALE":
		return models.GenderFemale
	default:
		return ""
	}
}

func optional(s string) *string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
