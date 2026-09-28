package customer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// fakeRepo is an in-memory Repository for unit tests.
type fakeRepo struct {
	mu         sync.Mutex
	customers  map[string]Customer
	screenings []Screening
	nextID     int
	failWith   error // if set, every call returns this error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{customers: map[string]Customer{}}
}

func (f *fakeRepo) newID() string {
	f.nextID++
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", f.nextID)
}

func (f *fakeRepo) Create(_ context.Context, in CreateInput) (Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return Customer{}, f.failWith
	}
	c := Customer{
		ID:          f.newID(),
		FullName:    in.FullName,
		DateOfBirth: in.DateOfBirth,
		Country:     in.Country,
		Status:      StatusPending,
		CreatedAt:   time.Now(),
	}
	f.customers[c.ID] = c
	return c, nil
}

func (f *fakeRepo) Get(_ context.Context, id string) (Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return Customer{}, f.failWith
	}
	c, ok := f.customers[id]
	if !ok {
		return Customer{}, ErrNotFound
	}
	return c, nil
}

func (f *fakeRepo) List(_ context.Context, limit, offset int) ([]Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return nil, f.failWith
	}
	all := make([]Customer, 0, len(f.customers))
	for _, c := range f.customers {
		all = append(all, c)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })
	if offset >= len(all) {
		return []Customer{}, nil
	}
	end := min(offset+limit, len(all))
	return all[offset:end], nil
}

func (f *fakeRepo) SaveScreening(_ context.Context, s Screening, newStatus Status) (Screening, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return Screening{}, f.failWith
	}
	c, ok := f.customers[s.CustomerID]
	if !ok {
		return Screening{}, ErrNotFound
	}
	c.Status = newStatus
	f.customers[c.ID] = c
	s.ID = f.newID()
	s.CreatedAt = time.Now()
	f.screenings = append(f.screenings, s)
	return s, nil
}

func (f *fakeRepo) ListScreenings(_ context.Context, customerID string) ([]Screening, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return nil, f.failWith
	}
	list := []Screening{}
	for i := len(f.screenings) - 1; i >= 0; i-- {
		if f.screenings[i].CustomerID == customerID {
			list = append(list, f.screenings[i])
		}
	}
	return list, nil
}

var errDatabaseDown = errors.New("database is down")
