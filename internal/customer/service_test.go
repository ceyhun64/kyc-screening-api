package customer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ceyhun64/kyc-screening-api/internal/screening"
)

// NEDEN yardımcı kurulum fonksiyonu: Her test aynı şekilde servis oluşturuyor; tekrar etmesin.
// Repo'yu da döndürüyorum ki testler içindeki veriyi kontrol edebilsin.
func newTestService() (*Service, *fakeRepo) {
	repo := newFakeRepo()
	svc := NewService(repo, screening.New(screening.DemoList, screening.DemoListVersion, 0.85))
	svc.now = func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) } // NEDEN sabit zaman: "gelecek tarih" testi hangi gün çalışırsa çalışsın aynı sonucu versin.
	return svc, repo
}

func TestCreateCustomerValidation(t *testing.T) {
	tests := []struct {
		name       string
		in         CreateInput
		wantFields []string // invalid fields we expect; empty means valid
	}{
		{"valid", CreateInput{"Ayşe Yılmaz", "1995-04-12", "tr"}, nil},
		{"name too short", CreateInput{"A", "1995-04-12", "TR"}, []string{"full_name"}},
		{"bad date format", CreateInput{"Ayşe Yılmaz", "12/04/1995", "TR"}, []string{"date_of_birth"}},
		{"date in the future", CreateInput{"Ayşe Yılmaz", "2030-01-01", "TR"}, []string{"date_of_birth"}},
		{"bad country", CreateInput{"Ayşe Yılmaz", "1995-04-12", "Turkey"}, []string{"country"}},
		{"everything wrong", CreateInput{"", "", ""}, []string{"full_name", "date_of_birth", "country"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestService()
			_, err := svc.CreateCustomer(context.Background(), tt.in)

			if len(tt.wantFields) == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("want *ValidationError, got %v", err)
			}
			if len(ve.Fields) != len(tt.wantFields) {
				t.Errorf("got invalid fields %v, want %v", ve.Fields, tt.wantFields)
			}
			for _, f := range tt.wantFields {
				if _, ok := ve.Fields[f]; !ok {
					t.Errorf("expected field %q to be invalid", f)
				}
			}
		})
	}
}

func TestCreateCustomerNormalizesInput(t *testing.T) {
	svc, _ := newTestService()
	c, err := svc.CreateCustomer(context.Background(), CreateInput{"  Ayşe   Yılmaz ", " 1995-04-12 ", " mt "})
	if err != nil {
		t.Fatal(err)
	}
	if c.FullName != "Ayşe Yılmaz" || c.Country != "MT" || c.Status != StatusPending {
		t.Errorf("unexpected customer: %+v", c)
	}
}

func TestScreenCustomer(t *testing.T) {
	tests := []struct {
		name       string
		fullName   string
		wantResult ScreeningResult
		wantStatus Status
	}{
		{"no match", "Ayşe Yılmaz", ResultClear, StatusClear},
		{"potential match", "Viktor Blackwood", ResultPotentialMatch, StatusReview},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newTestService()
			ctx := context.Background()

			c, err := svc.CreateCustomer(ctx, CreateInput{tt.fullName, "1980-01-01", "GB"})
			if err != nil {
				t.Fatal(err)
			}

			s, err := svc.ScreenCustomer(ctx, c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if s.Result != tt.wantResult {
				t.Errorf("result = %q, want %q", s.Result, tt.wantResult)
			}
			if s.ListVersion != screening.DemoListVersion {
				t.Errorf("list version = %q, want %q", s.ListVersion, screening.DemoListVersion)
			}
			if got := repo.customers[c.ID].Status; got != tt.wantStatus {
				t.Errorf("customer status = %q, want %q", got, tt.wantStatus)
			}
		})
	}
}

func TestScreenCustomerNotFound(t *testing.T) {
	svc, _ := newTestService()
	for _, id := range []string{"00000000-0000-0000-0000-000000000999", "not-a-uuid"} {
		_, err := svc.ScreenCustomer(context.Background(), id)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("id %q: want ErrNotFound, got %v", id, err)
		}
	}
}

// NEDEN bu test: Service hatayı sararken (%w) orijinal hatayı kaybetmemeli. errors.Is hâlâ bulabilmeli.
func TestRepositoryErrorsAreWrapped(t *testing.T) {
	svc, repo := newTestService()
	repo.failWith = errDatabaseDown

	_, err := svc.CreateCustomer(context.Background(), CreateInput{"Ayşe Yılmaz", "1995-04-12", "TR"})
	if !errors.Is(err, errDatabaseDown) {
		t.Fatalf("want wrapped errDatabaseDown, got %v", err)
	}
}

func TestListCustomersClampsLimit(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := svc.CreateCustomer(ctx, CreateInput{"Test Person", "1990-01-01", "MT"}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := svc.ListCustomers(ctx, 0, -5) // 0 → default, negative offset → 0
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("got %d customers, want 3", len(got))
	}
}
