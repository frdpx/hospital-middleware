package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"github.com/frdpx/hospital-middleware/internal/apierr"
)

var registerFieldNames sync.Once

func useJSONFieldNames() {
	registerFieldNames.Do(func() {
		validate, ok := binding.Validator.Engine().(*validator.Validate)
		if !ok {
			return
		}
		validate.RegisterTagNameFunc(func(field reflect.StructField) string {
			name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
			if name == "" || name == "-" {
				return field.Name
			}
			return name
		})
	})
}

type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func respondError(c *gin.Context, logger *slog.Logger, err error) {
	apiErr := apierr.From(err)

	if apiErr.Status >= http.StatusInternalServerError {
		logger.ErrorContext(c.Request.Context(), "request failed",
			"code", apiErr.Code,
			"path", c.FullPath(),
			"error", apiErr.Error(),
		)
	} else {
		logger.DebugContext(c.Request.Context(), "request rejected",
			"code", apiErr.Code,
			"path", c.FullPath(),
			"message", apiErr.Message,
		)
	}

	c.AbortWithStatusJSON(apiErr.Status, errorEnvelope{
		Error: errorDetail{Code: apiErr.Code, Message: apiErr.Message},
	})
}

func bindJSON(c *gin.Context, target any) error {
	if err := c.ShouldBindJSON(target); err != nil {
		return apierr.Validation(validationMessage(err)).WithCause(err)
	}
	return nil
}

func validationMessage(err error) string {
	var validationErrs validator.ValidationErrors
	if errors.As(err, &validationErrs) && len(validationErrs) > 0 {
		fieldErr := validationErrs[0]
		field := fieldErr.Field()
		switch fieldErr.Tag() {
		case "required":
			return field + " is required"
		case "min":
			return field + " must be at least " + fieldErr.Param() + " characters"
		case "max":
			return field + " must be at most " + fieldErr.Param() + " characters"
		case "email":
			return field + " must be a valid email address"
		case "datetime":
			return field + " must be in YYYY-MM-DD format"
		default:
			return field + " is invalid"
		}
	}
	return "request body is not valid JSON for this endpoint"
}
