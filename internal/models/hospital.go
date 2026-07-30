package models

import (
	"time"

	"github.com/google/uuid"
)

// HISAdapterType selects which HISClient implementation serves a hospital.
// Adding "Hospital B" later means adding a constant and a client, not a schema
// change.
type HISAdapterType string

const (
	HISAdapterHospitalA HISAdapterType = "hospital_a"
)

// Hospital is reference data: rows are provisioned by an operator (see the
// seed migration), never created through the public API.
type Hospital struct {
	ID             uuid.UUID      `json:"id"`
	Code           string         `json:"code"`
	Name           string         `json:"name"`
	HISAdapterType HISAdapterType `json:"his_adapter_type"`
	HISBaseURL     *string        `json:"his_base_url"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}
