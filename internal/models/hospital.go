package models

import (
	"time"

	"github.com/google/uuid"
)

type HISAdapterType string

const (
	HISAdapterHospitalA HISAdapterType = "hospital_a"
)

type Hospital struct {
	ID             uuid.UUID      `json:"id"`
	Code           string         `json:"code"`
	Name           string         `json:"name"`
	HISAdapterType HISAdapterType `json:"his_adapter_type"`
	HISBaseURL     *string        `json:"his_base_url"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}
