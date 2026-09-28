package customer

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
)

// Handler exposes the service over HTTP.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler creates the HTTP handlers.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Register adds the routes to mux. It uses the method and path patterns
// that net/http supports since Go 1.22, so no router library is needed.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/customers", h.create)
	mux.HandleFunc("GET /v1/customers", h.list)
	mux.HandleFunc("GET /v1/customers/{id}", h.get)
	mux.HandleFunc("POST /v1/customers/{id}/screenings", h.screen)
	mux.HandleFunc("GET /v1/customers/{id}/screenings", h.listScreenings)
}

const maxBodyBytes = 1 << 20 // 1 MB

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var in CreateInput
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_json", "request body must be valid JSON", nil))
		return
	}

	c, err := h.svc.CreateCustomer(r.Context(), in)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/customers/"+c.ID)
	writeJSON(w, http.StatusCreated, c)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.GetCustomer(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	limit, err1 := queryInt(r, "limit")
	offset, err2 := queryInt(r, "offset")
	if err1 != nil || err2 != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_query", "limit and offset must be integers", nil))
		return
	}

	cs, err := h.svc.ListCustomers(r.Context(), limit, offset)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": cs})
}

func (h *Handler) screen(w http.ResponseWriter, r *http.Request) {
	s, err := h.svc.ScreenCustomer(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s)
}

func (h *Handler) listScreenings(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.ListScreenings(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": list})
}

// writeError turns a service error into an HTTP response.
// Unknown errors are logged and hidden from the client.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, errorBody("validation_failed", "some fields are invalid", ve.Fields))
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody("not_found", "customer not found", nil))
	default:
		h.log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody("internal_error", "something went wrong", nil))
	}
}

func queryInt(r *http.Request, key string) (int, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return 0, nil
	}
	return strconv.Atoi(v)
}

type apiError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func errorBody(code, message string, fields map[string]string) map[string]apiError {
	return map[string]apiError{"error": {Code: code, Message: message, Fields: fields}}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
