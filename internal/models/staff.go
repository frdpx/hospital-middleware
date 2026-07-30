package models

import (
	"time"

	"github.com/google/uuid"
)

// Staff is a hospital employee account. A staff member belongs to exactly one
// hospital, and that hospital is the only data scope they can ever read.
//
// PasswordHash is deliberately json:"-": this struct is returned from the
// repository layer, and tagging it here means no future handler can leak the
// hash by accident.
type Staff struct {
	ID           uuid.UUID `json:"id"`
	HospitalID   uuid.UUID `json:"hospital_id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
