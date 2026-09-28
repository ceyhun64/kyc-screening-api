package customer

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestMux() (*http.ServeMux, *fakeRepo) {
	svc, repo := newTestService()
	mux := http.NewServeMux()
	NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil))).Register(mux)
	return mux, repo
}

func do(t *testing.T, mux http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestCreateAndGetCustomer(t *testing.T) {
	mux, _ := newTestMux()

	rec := do(t, mux, "POST", "/v1/customers", `{"full_name":"Ayşe Yılmaz","date_of_birth":"1995-04-12","country":"TR"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body)
	}
	var created Customer
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if loc := rec.Header().Get("Location"); loc != "/v1/customers/"+created.ID {
		t.Errorf("Location = %q", loc)
	}

	rec = do(t, mux, "GET", "/v1/customers/"+created.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestHandlerErrors(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		path     string
		body     string
		wantCode int
		wantErr  string
	}{
		{"invalid json", "POST", "/v1/customers", `{not json`, 400, "invalid_json"},
		{"unknown field", "POST", "/v1/customers", `{"full_name":"Ayşe Yılmaz","date_of_birth":"1995-04-12","country":"TR","admin":true}`, 400, "invalid_json"},
		{"validation", "POST", "/v1/customers", `{"full_name":"A","date_of_birth":"x","country":"TR"}`, 400, "validation_failed"},
		{"unknown customer", "GET", "/v1/customers/00000000-0000-0000-0000-000000000999", "", 404, "not_found"},
		{"id is not a uuid", "GET", "/v1/customers/abc", "", 404, "not_found"},
		{"screen unknown customer", "POST", "/v1/customers/00000000-0000-0000-0000-000000000999/screenings", "", 404, "not_found"},
		{"bad limit", "GET", "/v1/customers?limit=ten", "", 400, "invalid_query"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux, _ := newTestMux()
			rec := do(t, mux, tt.method, tt.path, tt.body)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.wantCode, rec.Body)
			}
			var body map[string]apiError
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if got := body["error"].Code; got != tt.wantErr {
				t.Errorf("error code = %q, want %q", got, tt.wantErr)
			}
		})
	}
}

func TestInternalErrorsAreHidden(t *testing.T) {
	mux, repo := newTestMux()
	repo.failWith = errDatabaseDown

	rec := do(t, mux, "GET", "/v1/customers", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "database is down") {
		t.Error("internal error details leaked to the client")
	}
}

func TestScreeningFlow(t *testing.T) {
	mux, _ := newTestMux()

	rec := do(t, mux, "POST", "/v1/customers", `{"full_name":"Viktor Blackwood","date_of_birth":"1970-03-01","country":"GB"}`)
	var c Customer
	_ = json.NewDecoder(rec.Body).Decode(&c)

	rec = do(t, mux, "POST", "/v1/customers/"+c.ID+"/screenings", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("screen: status = %d, body = %s", rec.Code, rec.Body)
	}
	var s Screening
	_ = json.NewDecoder(rec.Body).Decode(&s)
	if s.Result != ResultPotentialMatch {
		t.Errorf("result = %q, want %q", s.Result, ResultPotentialMatch)
	}

	rec = do(t, mux, "GET", "/v1/customers/"+c.ID, "")
	_ = json.NewDecoder(rec.Body).Decode(&c)
	if c.Status != StatusReview {
		t.Errorf("status = %q, want %q", c.Status, StatusReview)
	}

	rec = do(t, mux, "GET", "/v1/customers/"+c.ID+"/screenings", "")
	var history struct{ Data []Screening }
	_ = json.NewDecoder(rec.Body).Decode(&history)
	if len(history.Data) != 1 {
		t.Errorf("history has %d screenings, want 1", len(history.Data))
	}
}
