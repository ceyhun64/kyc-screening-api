package customer

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ceyhun64/kyc-screening-api/internal/database"
)

// These tests run against a real PostgreSQL database. They are skipped unless
// TEST_DATABASE_URL is set, e.g.:
//
//	TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/kyc_test?sslmode=disable go test ./...
//
// NEDEN gerçek veritabanıyla test: SQL'i mock'lamak gerçek hataları gizler (yanlış kolon adı,
// NULL işleme, transaction davranışı). Repository'yi ancak gerçek PostgreSQL doğru test eder.
// NEDEN t.Skip: Veritabanı yoksa (ör. geliştiricinin bilgisayarında) testler başarısız değil,
// atlanmış sayılıyor. CI'da TEST_DATABASE_URL ayarlı olduğu için orada mutlaka çalışıyorlar.
func newTestRepo(t *testing.T) *PostgresRepository {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() }) // NEDEN t.Cleanup: Test bitince (başarılı ya da değil) bağlantı kapansın.

	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	// NEDEN TRUNCATE: Her test temiz bir veritabanıyla başlasın; testler birbirini etkilemesin.
	if _, err := db.ExecContext(ctx, `TRUNCATE screenings, customers`); err != nil {
		t.Fatal(err)
	}
	return NewPostgresRepository(db)
}

func TestPostgresRepository(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	c, err := repo.Create(ctx, CreateInput{FullName: "Ayşe Yılmaz", DateOfBirth: "1995-04-12", Country: "TR"})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == "" || c.Status != StatusPending || c.DateOfBirth != "1995-04-12" {
		t.Fatalf("unexpected customer: %+v", c)
	}

	got, err := repo.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FullName != "Ayşe Yılmaz" {
		t.Errorf("full name = %q", got.FullName)
	}

	s, err := repo.SaveScreening(ctx, Screening{
		CustomerID:  c.ID,
		Result:      ResultPotentialMatch,
		MatchedName: "Someone Else",
		Score:       0.9,
		ListVersion: "test-v1",
	}, StatusReview)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID == "" || s.CreatedAt.IsZero() {
		t.Errorf("screening not saved: %+v", s)
	}

	got, _ = repo.Get(ctx, c.ID)
	if got.Status != StatusReview {
		t.Errorf("status = %q, want %q", got.Status, StatusReview)
	}

	history, err := repo.ListScreenings(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].MatchedName != "Someone Else" {
		t.Errorf("unexpected history: %+v", history)
	}

	list, err := repo.List(ctx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("list has %d customers, want 1", len(list))
	}
}

func TestPostgresNotFound(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	missing := "00000000-0000-0000-0000-000000000999"

	if _, err := repo.Get(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: want ErrNotFound, got %v", err)
	}
	_, err := repo.SaveScreening(ctx, Screening{CustomerID: missing, Result: ResultClear, ListVersion: "v"}, StatusClear)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("SaveScreening: want ErrNotFound, got %v", err)
	}
}
