package models

import (
	"time"

	"github.com/google/uuid"
)

type Staff struct {
	ID           uuid.UUID `json:"id"`
	HospitalID   uuid.UUID `json:"hospital_id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
