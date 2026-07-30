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

// maxHISResponseBytes caps how much of an upstream response we will read. A
// misbehaving or compromised HIS must not be able to exhaust our memory.
const maxHISResponseBytes = 1 << 20 // 1 MiB

// hospitalAResponse mirrors the documented Hospital A payload verbatim. It
// stays unexported: nothing outside this file should depend on Hospital A's
// field shape.
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

// HospitalAClient talks to the Hospital A HIS:
//
//	GET {baseURL}/patient/search/{id}
type HospitalAClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewHospitalAClient builds a client for the given HIS base URL. A nil
// httpClient falls back to a client with a conservative timeout, because an
// http.Client without one waits forever and would pin our request goroutines.
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

	// PathEscape, not raw concatenation: a passport id containing "/" or ".."
	// must not be able to redirect the request to another HIS endpoint.
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

// toProfile normalizes Hospital A's payload into our internal model.
func (r hospitalAResponse) toProfile() (*PatientProfile, error) {
	nationalID := optional(r.NationalID)
	passportID := optional(r.PassportID)

	// Without at least one globally unique identifier we cannot dedupe this
	// person across hospitals, and the patients table would reject the row.
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

// hisDateLayouts covers the formats a HIS has been seen to emit for a date.
// The spec only documents YYYY-MM-DD; the others are accepted defensively
// rather than failing an otherwise valid patient record.
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

// normalizeGender maps the HIS value onto our M/F domain, tolerating case and
// the spelled-out forms. Anything unrecognized becomes empty rather than
// failing the whole record.
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

// optional converts a HIS empty string into a NULL-able nil pointer, so
// "absent" and "empty" do not both end up stored as the same empty string.
func optional(s string) *string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
