package models_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/frdpx/hospital-middleware/internal/models"
)

func TestDate_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	type payload struct {
		DOB models.Date `json:"date_of_birth"`
	}

	var decoded payload
	require.NoError(t, json.Unmarshal([]byte(`{"date_of_birth":"1990-05-20"}`), &decoded))
	assert.Equal(t, "1990-05-20", decoded.DOB.String())

	encoded, err := json.Marshal(decoded)
	require.NoError(t, err)
	assert.JSONEq(t, `{"date_of_birth":"1990-05-20"}`, string(encoded),
		"a date must never serialize with a meaningless 00:00:00 time component")
}

func TestParseDate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "ISO date", input: "1990-05-20"},
		{name: "leap day", input: "2024-02-29"},
		{name: "day/month/year is not accepted", input: "20/05/1990", wantErr: true},
		{name: "RFC3339 is not a plain date", input: "1990-05-20T00:00:00Z", wantErr: true},
		{name: "impossible date", input: "1990-13-45", wantErr: true},
		{name: "empty string", input: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := models.ParseDate(tc.input)

			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.input, got.String())
		})
	}
}

func TestDate_ScanAndValue(t *testing.T) {
	t.Parallel()

	t.Run("scans the time.Time a driver returns for a DATE column", func(t *testing.T) {
		t.Parallel()

		var d models.Date
		require.NoError(t, d.Scan(time.Date(1990, 5, 20, 13, 45, 0, 0, time.UTC)))
		assert.Equal(t, "1990-05-20", d.String(), "the clock portion is discarded")
	})

	t.Run("scans a NULL into the zero value", func(t *testing.T) {
		t.Parallel()

		var d models.Date
		require.NoError(t, d.Scan(nil))
		assert.True(t, d.IsZero())

		value, err := d.Value()
		require.NoError(t, err)
		assert.Nil(t, value, "a zero date is stored as NULL, not as year 1")
	})

	t.Run("rejects a type it cannot represent", func(t *testing.T) {
		t.Parallel()

		var d models.Date
		assert.Error(t, d.Scan(42))
	})
}

func TestPatientSearchCriteria(t *testing.T) {
	t.Parallel()

	ptr := func(s string) *string { return &s }

	t.Run("no filters at all is empty", func(t *testing.T) {
		t.Parallel()
		assert.True(t, models.PatientSearchCriteria{}.IsEmpty())
	})

	t.Run("a single filter is not empty", func(t *testing.T) {
		t.Parallel()
		assert.False(t, models.PatientSearchCriteria{LastName: ptr("Doe")}.IsEmpty())
	})

	t.Run("only identifier searches can reach the HIS", func(t *testing.T) {
		t.Parallel()

		assert.False(t, models.PatientSearchCriteria{LastName: ptr("Doe")}.HasIdentifier())
		assert.True(t, models.PatientSearchCriteria{NationalID: ptr("123")}.HasIdentifier())
		assert.True(t, models.PatientSearchCriteria{PassportID: ptr("AA1")}.HasIdentifier())
	})

	t.Run("national id wins when both identifiers are supplied", func(t *testing.T) {
		t.Parallel()

		criteria := models.PatientSearchCriteria{NationalID: ptr("123"), PassportID: ptr("AA1")}
		assert.Equal(t, "123", criteria.Identifier())
		assert.Empty(t, models.PatientSearchCriteria{}.Identifier())
	})
}
