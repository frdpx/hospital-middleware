package mockhis

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

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

type Server struct {
	mu       sync.RWMutex
	patients map[string]Patient
}

func New() *Server {
	s := &Server{patients: map[string]Patient{}}
	for _, p := range SeedPatients() {
		s.Add(p)
	}
	return s
}

func NewEmpty() *Server {
	return &Server{patients: map[string]Patient{}}
}

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
