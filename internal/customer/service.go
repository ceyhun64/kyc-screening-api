package customer

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyhun64/kyc-screening-api/internal/screening"
)

// Repository is what the service needs from storage. It is defined here,
// where it is used, so the service does not depend on PostgreSQL and tests
// can use an in-memory fake.
type Repository interface {
	Create(ctx context.Context, in CreateInput) (Customer, error)
	Get(ctx context.Context, id string) (Customer, error)
	List(ctx context.Context, limit, offset int) ([]Customer, error)
	// SaveScreening stores the screening and updates the customer's status
	// in one transaction.
	SaveScreening(ctx context.Context, s Screening, newStatus Status) (Screening, error)
	ListScreenings(ctx context.Context, customerID string) ([]Screening, error)
}

// Screener checks a name against a sanctions list.
type Screener interface {
	Screen(name string) screening.Result
}

// Service holds the business logic.
type Service struct {
	repo     Repository
	screener Screener
	now      func() time.Time
}

// NewService wires the service with its dependencies.
func NewService(repo Repository, screener Screener) *Service {
	return &Service{repo: repo, screener: screener, now: time.Now}
}

const (
	defaultLimit = 20
	maxLimit     = 100
)

// CreateCustomer validates the input and stores a new customer.
func (s *Service) CreateCustomer(ctx context.Context, in CreateInput) (Customer, error) {
	in = in.normalize()
	if err := in.validate(s.now()); err != nil {
		return Customer{}, err
	}

	c, err := s.repo.Create(ctx, in)
	if err != nil {
		return Customer{}, fmt.Errorf("create customer: %w", err)
	}
	return c, nil
}

// GetCustomer returns one customer, or ErrNotFound.
func (s *Service) GetCustomer(ctx context.Context, id string) (Customer, error) {
	if !validID(id) {
		return Customer{}, ErrNotFound
	}
	c, err := s.repo.Get(ctx, id)
	if err != nil {
		return Customer{}, fmt.Errorf("get customer %s: %w", id, err)
	}
	return c, nil
}

// ListCustomers returns a page of customers, newest first.
func (s *Service) ListCustomers(ctx context.Context, limit, offset int) ([]Customer, error) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if offset < 0 {
		offset = 0
	}

	cs, err := s.repo.List(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list customers: %w", err)
	}
	return cs, nil
}

// ScreenCustomer checks the customer against the sanctions list, stores the
// result and updates the customer's status.
func (s *Service) ScreenCustomer(ctx context.Context, id string) (Screening, error) {
	c, err := s.GetCustomer(ctx, id)
	if err != nil {
		return Screening{}, fmt.Errorf("screen customer: %w", err)
	}

	r := s.screener.Screen(c.FullName)

	sc := Screening{
		CustomerID:  c.ID,
		Result:      ResultClear,
		Score:       r.Score,
		ListVersion: r.ListVersion,
	}
	status := StatusClear
	if r.Match {
		// We never decide "sanctioned" automatically. A possible match
		// goes to a human for review.
		sc.Result = ResultPotentialMatch
		sc.MatchedName = r.MatchedName
		status = StatusReview
	}

	saved, err := s.repo.SaveScreening(ctx, sc, status)
	if err != nil {
		return Screening{}, fmt.Errorf("save screening for %s: %w", id, err)
	}
	return saved, nil
}

// ListScreenings returns the screening history of a customer, newest first.
func (s *Service) ListScreenings(ctx context.Context, id string) ([]Screening, error) {
	if _, err := s.GetCustomer(ctx, id); err != nil {
		return nil, err
	}
	list, err := s.repo.ListScreenings(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list screenings for %s: %w", id, err)
	}
	return list, nil
}
