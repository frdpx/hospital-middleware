// Package mockhis serves a stand-in for the Hospital A HIS.
//
// The real endpoint (https://hospital-a.api.co.th) is not reachable from a
// development machine or from CI, so this package implements the same contract
// over HTTP. It is a plain http.Handler rather than an httptest.Server so the
// exact same code can back both an httptest server in unit tests and the
// standalone `cmd/mockhis` binary used by docker-compose.
package mockhis

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// Patient mirrors the documented Hospital A response body field for field.
type Patient struct {
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

// Server is an in-memory Hospital A HIS.
type Server struct {
	mu       sync.RWMutex
	patients map[string]Patient // keyed by national_id and passport_id
}

// New returns a server preloaded with SeedPatients.
func New() *Server {
	s := &Server{patients: map[string]Patient{}}
	for _, p := range SeedPatients() {
		s.Add(p)
	}
	return s
}

// NewEmpty returns a server with no patients, for tests that seed their own.
func NewEmpty() *Server {
	return &Server{patients: map[string]Patient{}}
}

// Add indexes a patient under every identifier it carries.
func (s *Server) Add(p Patient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.NationalID != "" {
		s.patients[p.NationalID] = p
	}
	if p.PassportID != "" {
		s.patients[p.PassportID] = p
	}
}

// Handler routes GET /patient/search/{id}, matching the real HIS contract.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /patient/search/{id}", s.handleSearch)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))

	s.mu.RLock()
	patient, ok := s.patients[id]
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"patient not found"}`))
		return
	}
	_ = json.NewEncoder(w).Encode(patient)
}

// SeedPatients is the demo dataset. The identifiers here are the ones the
// README and the API spec examples use, so a reviewer can copy-paste a
// /patient/search request and get a hit on a clean database.
func SeedPatients() []Patient {
	return []Patient{
		{
			FirstNameTH: "สมชาย", LastNameTH: "ใจดี",
			FirstNameEN: "Somchai", LastNameEN: "Jaidee",
			DateOfBirth: "1990-05-20", PatientHN: "HN00123",
			NationalID:  "1234567890123",
			PhoneNumber: "0812345678", Email: "somchai.jaidee@example.com",
			Gender: "M",
		},
		{
			FirstNameTH: "สมหญิง", MiddleNameTH: "ศรี", LastNameTH: "รักษ์ดี",
			FirstNameEN: "Somying", MiddleNameEN: "Sri", LastNameEN: "Rakdee",
			DateOfBirth: "1985-11-02", PatientHN: "HN00456",
			NationalID:  "9876543210987",
			PhoneNumber: "0898765432", Email: "somying.rakdee@example.com",
			Gender: "F",
		},
		{
			FirstNameTH: "จอห์น", LastNameTH: "โด",
			FirstNameEN: "John", LastNameEN: "Doe",
			DateOfBirth: "1978-01-15", PatientHN: "HN00789",
			PassportID:  "AA1234567",
			PhoneNumber: "0855555555", Email: "john.doe@example.com",
			Gender: "M",
		},
	}
}
