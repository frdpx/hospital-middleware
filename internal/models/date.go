package models

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"
)

const DateLayout = "2006-01-02"

type Date struct {
	time.Time
}

func NewDate(t time.Time) Date {
	return Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)}
}

func ParseDate(s string) (Date, error) {
	t, err := time.Parse(DateLayout, s)
	if err != nil {
		return Date{}, fmt.Errorf("date must be in %s format: %w", DateLayout, err)
	}
	return Date{Time: t}, nil
}

func (d Date) String() string { return d.Format(DateLayout) }

func (d Date) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.Format(DateLayout) + `"`), nil
}

func (d *Date) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		*d = Date{}
		return nil
	}
	parsed, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

func (d Date) Value() (driver.Value, error) {
	if d.IsZero() {
		return nil, nil
	}
	return d.Time, nil
}

func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*d = Date{}
		return nil
	case time.Time:
		*d = NewDate(v)
		return nil
	case string:
		parsed, err := ParseDate(v)
		if err != nil {
			return err
		}
		*d = parsed
		return nil
	case []byte:
		parsed, err := ParseDate(string(v))
		if err != nil {
			return err
		}
		*d = parsed
		return nil
	default:
		return fmt.Errorf("cannot scan %T into models.Date", src)
	}
}
