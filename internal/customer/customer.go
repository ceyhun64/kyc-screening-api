// Package customer contains the customer onboarding and screening feature:
// domain types, business logic, the HTTP handlers and the PostgreSQL
// repository. Everything for one feature lives in one package.
package customer

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Status is the compliance status of a customer.
type Status string

const (
	StatusPending Status = "pending" // not screened yet
	StatusClear   Status = "clear"   // screened, no match found
	StatusReview  Status = "review"  // potential match, needs a human to review
)

// Customer is a person being onboarded.
type Customer struct {
	ID          string    `json:"id"`
	FullName    string    `json:"full_name"`
	DateOfBirth string    `json:"date_of_birth"` // YYYY-MM-DD
	Country     string    `json:"country"`       // ISO 3166-1 alpha-2, e.g. "MT"
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

// ScreeningResult is the outcome of one screening run.
type ScreeningResult string

const (
	ResultClear          ScreeningResult = "clear"
	ResultPotentialMatch ScreeningResult = "potential_match"
)

// Screening records one check of a customer against the sanctions list.
// Screenings are never updated or deleted: they are the audit trail.
type Screening struct {
	ID          string          `json:"id"`
	CustomerID  string          `json:"customer_id"`
	Result      ScreeningResult `json:"result"`
	MatchedName string          `json:"matched_name,omitempty"`
	Score       float64         `json:"score"`
	ListVersion string          `json:"list_version"`
	CreatedAt   time.Time       `json:"created_at"`
}

// CreateInput is the data needed to create a customer.
type CreateInput struct {
	FullName    string `json:"full_name"`
	DateOfBirth string `json:"date_of_birth"`
	Country     string `json:"country"`
}

// ErrNotFound is returned when a customer does not exist.
var ErrNotFound = errors.New("not found")

// ValidationError lists the invalid fields and why they are invalid.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", k, e.Fields[k]))
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)

// normalize cleans up the input before validation.
func (in CreateInput) normalize() CreateInput {
	return CreateInput{
		FullName:    strings.Join(strings.Fields(in.FullName), " "),
		DateOfBirth: strings.TrimSpace(in.DateOfBirth),
		Country:     strings.ToUpper(strings.TrimSpace(in.Country)),
	}
}

// validate checks the input. now is passed in so tests don't depend on the clock.
func (in CreateInput) validate(now time.Time) error {
	fields := map[string]string{}

	if n := len([]rune(in.FullName)); n < 2 || n > 200 {
		fields["full_name"] = "must be between 2 and 200 characters"
	}

	dob, err := time.Parse("2006-01-02", in.DateOfBirth)
	switch {
	case err != nil:
		fields["date_of_birth"] = "must be a date in YYYY-MM-DD format"
	case !dob.Before(now):
		fields["date_of_birth"] = "must be in the past"
	}

	if !countryCode.MatchString(in.Country) {
		fields["country"] = "must be a 2-letter ISO country code"
	}

	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// validID reports whether id looks like a UUID. Checking this early turns
// "/customers/abc" into a clean 404 instead of a database error.
func validID(id string) bool {
	return uuidPattern.MatchString(id)
}
