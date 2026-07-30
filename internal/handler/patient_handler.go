package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/bambam/hospital-middleware/internal/apierr"
	"github.com/bambam/hospital-middleware/internal/middleware"
	"github.com/bambam/hospital-middleware/internal/models"
	"github.com/bambam/hospital-middleware/internal/service"
)

type PatientHandler struct {
	patients *service.PatientService
	logger   *slog.Logger
}

func NewPatientHandler(patients *service.PatientService, logger *slog.Logger) *PatientHandler {
	return &PatientHandler{patients: patients, logger: logger}
}

type searchPatientRequest struct {
	NationalID  *string `json:"national_id"`
	PassportID  *string `json:"passport_id"`
	FirstName   *string `json:"first_name"`
	MiddleName  *string `json:"middle_name"`
	LastName    *string `json:"last_name"`
	DateOfBirth *string `json:"date_of_birth" binding:"omitempty,datetime=2006-01-02"`
	PhoneNumber *string `json:"phone_number"`
	Email       *string `json:"email"`
}

type patientResponse struct {
	PatientHN    string  `json:"patient_hn"`
	NationalID   *string `json:"national_id"`
	PassportID   *string `json:"passport_id"`
	FirstNameTH  string  `json:"first_name_th"`
	MiddleNameTH *string `json:"middle_name_th"`
	LastNameTH   string  `json:"last_name_th"`
	FirstNameEN  string  `json:"first_name_en"`
	MiddleNameEN *string `json:"middle_name_en"`
	LastNameEN   string  `json:"last_name_en"`
	DateOfBirth  *string `json:"date_of_birth"`
	PhoneNumber  *string `json:"phone_number"`
	Email        *string `json:"email"`
	Gender       string  `json:"gender"`
}

type searchPatientResponse struct {
	Results []patientResponse `json:"results"`
	Count   int               `json:"count"`
}

func (h *PatientHandler) Search(c *gin.Context) {
	claims, ok := middleware.ClaimsFrom(c)
	if !ok {
		respondError(c, h.logger, apierr.Unauthorized("a valid bearer token is required"))
		return
	}

	var req searchPatientRequest
	if err := bindJSON(c, &req); err != nil {
		respondError(c, h.logger, err)
		return
	}

	criteria, err := req.toCriteria()
	if err != nil {
		respondError(c, h.logger, err)
		return
	}
	criteria.HospitalID = claims.HospitalID

	records, err := h.patients.Search(c.Request.Context(), criteria)
	if err != nil {
		respondError(c, h.logger, err)
		return
	}

	results := make([]patientResponse, 0, len(records))
	for _, record := range records {
		results = append(results, toPatientResponse(record))
	}
	c.JSON(http.StatusOK, searchPatientResponse{Results: results, Count: len(results)})
}

func (r searchPatientRequest) toCriteria() (models.PatientSearchCriteria, error) {
	criteria := models.PatientSearchCriteria{
		NationalID:  trimmed(r.NationalID),
		PassportID:  trimmed(r.PassportID),
		FirstName:   trimmed(r.FirstName),
		MiddleName:  trimmed(r.MiddleName),
		LastName:    trimmed(r.LastName),
		PhoneNumber: trimmed(r.PhoneNumber),
		Email:       trimmed(r.Email),
	}

	if dob := trimmed(r.DateOfBirth); dob != nil {
		parsed, err := models.ParseDate(*dob)
		if err != nil {
			return criteria, apierr.Validation("date_of_birth must be in YYYY-MM-DD format").WithCause(err)
		}
		criteria.DateOfBirth = &parsed
	}
	return criteria, nil
}

func trimmed(value *string) *string {
	if value == nil {
		return nil
	}
	v := strings.TrimSpace(*value)
	if v == "" {
		return nil
	}
	return &v
}

func toPatientResponse(record models.PatientRecord) patientResponse {
	var dob *string
	if record.DateOfBirth != nil && !record.DateOfBirth.IsZero() {
		formatted := record.DateOfBirth.String()
		dob = &formatted
	}

	return patientResponse{
		PatientHN:    record.PatientHN,
		NationalID:   record.NationalID,
		PassportID:   record.PassportID,
		FirstNameTH:  record.FirstNameTH,
		MiddleNameTH: record.MiddleNameTH,
		LastNameTH:   record.LastNameTH,
		FirstNameEN:  record.FirstNameEN,
		MiddleNameEN: record.MiddleNameEN,
		LastNameEN:   record.LastNameEN,
		DateOfBirth:  dob,
		PhoneNumber:  record.PhoneNumber,
		Email:        record.Email,
		Gender:       record.Gender,
	}
}
