package customer

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
)

// Handler exposes the service over HTTP.
// NEDEN handler ince (thin): Handler sadece HTTP işini yapıyor: JSON oku, service'i çağır,
// sonucu JSON olarak yaz. İş kuralı yok. Böylece iş mantığı HTTP'den bağımsız test edilebiliyor.
type Handler struct {
	svc *Service
	log *slog.Logger // NEDEN logger dışarıdan geliyor: Global logger yerine enjekte edilince testte log'ları susturabiliyorum.
}

// NewHandler creates the HTTP handlers.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Register adds the routes to mux. It uses the method and path patterns
// that net/http supports since Go 1.22, so no router library is needed.
//
// NEDEN /v1 öneki: API versiyonlama. İleride uyumsuz bir değişiklik gerekirse /v2 açılır,
// /v1'i kullanan eski istemciler bozulmaz.
// NEDEN taramalar alt kaynak (/customers/{id}/screenings): Tarama bir müşteriye ait.
// REST'te bu ilişki URL yapısıyla ifade edilir.
// NEDEN tarama POST: Her çağrı yeni bir tarama kaydı oluşturuyor, yani yeni bir kaynak yaratılıyor.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/customers", h.create)
	mux.HandleFunc("GET /v1/customers", h.list)
	mux.HandleFunc("GET /v1/customers/{id}", h.get)
	mux.HandleFunc("POST /v1/customers/{id}/screenings", h.screen)
	mux.HandleFunc("GET /v1/customers/{id}/screenings", h.listScreenings)
}

// NEDEN 1 MB sınır: Biri çok büyük bir gövde gönderip sunucunun belleğini doldurmasın.
// Bu API için 1 MB fazlasıyla yeterli. "1 << 20" = 2^20 bayt = 1 MB.
const maxBodyBytes = 1 << 20

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var in CreateInput
	dec := json.NewDecoder(r.Body)
	// NEDEN bilinmeyen alanları reddediyorum: İstemci "status":"clear" veya "admin":true gibi
	// beklenmeyen alanlar gönderirse sessizce yok saymak yerine hata veriyorum. Yazım hatalarını
	// (ör. "fullname") hemen fark ettiriyor ve beklenmedik veri kabul etmiyor.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_json", "request body must be valid JSON", nil))
		return
	}

	// NEDEN r.Context(): İstemci bağlantıyı kesersek bu context iptal olur ve veritabanı sorgusu da durur.
	c, err := h.svc.CreateCustomer(r.Context(), in)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	// NEDEN Location header + 201: REST standardı. Yeni kaynak oluşturulunca 201 Created dönülür
	// ve Location header'ı yeni kaynağın adresini gösterir.
	w.Header().Set("Location", "/v1/customers/"+c.ID)
	writeJSON(w, http.StatusCreated, c)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	// NEDEN r.PathValue: Go 1.22 ile gelen, route'taki {id} parametresini okuyan yöntem.
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
		// NEDEN 400: "limit=ten" gibi sayı olmayan bir değer istemcinin hatası; varsayılana çekmek yerine açıkça bildiriyorum.
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_query", "limit and offset must be integers", nil))
		return
	}

	cs, err := h.svc.ListCustomers(r.Context(), limit, offset)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	// NEDEN {"data": [...]} ile sarıyorum: Listeyi doğrudan dizi olarak dönmek yerine bir nesne içine koyunca,
	// ileride "total", "next_cursor" gibi alanlar eklemek API'yi bozmadan mümkün oluyor.
	writeJSON(w, http.StatusOK, map[string]any{"data": cs})
}

func (h *Handler) screen(w http.ResponseWriter, r *http.Request) {
	s, err := h.svc.ScreenCustomer(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s) // NEDEN 201: Yeni bir tarama kaydı oluştu.
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
//
// NEDEN tek bir hata eşleme fonksiyonu: Hangi hatanın hangi HTTP koduna dönüştüğü tek yerde.
// Her handler'da ayrı ayrı if/else yazmıyorum, tutarlılık garanti.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *ValidationError
	switch {
	// NEDEN errors.As: Hata birkaç kez sarılmış olsa bile (fmt.Errorf %w) zincirin içinde
	// *ValidationError tipinde bir hata var mı diye bakar ve varsa ve'ye atar.
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, errorBody("validation_failed", "some fields are invalid", ve.Fields))
	// NEDEN errors.Is: Zincirin içinde ErrNotFound değeri var mı diye bakar. err == ErrNotFound
	// yazsaydım, sarılmış hatalarda çalışmazdı.
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody("not_found", "customer not found", nil))
	default:
		// NEDEN iç hatayı istemciye göstermiyorum: Veritabanı hata mesajları tablo adları, SQL gibi
		// iç bilgiler içerebilir, bu güvenlik açığı olur. Detayı loga yazıyorum, istemciye genel mesaj dönüyorum.
		h.log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody("internal_error", "something went wrong", nil))
	}
}

// NEDEN parametre yoksa 0 dönüyor: 0 değeri service'te "varsayılanı kullan" anlamına geliyor.
func queryInt(r *http.Request, key string) (int, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return 0, nil
	}
	return strconv.Atoi(v)
}

// NEDEN tüm hatalar aynı yapıda: İstemci her hatayı aynı şekilde işleyebilir.
// "code" makine için (kod içinde kontrol edilir), "message" insan için, "fields" form hataları için.
type apiError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func errorBody(code, message string, fields map[string]string) map[string]apiError {
	return map[string]apiError{"error": {Code: code, Message: message, Fields: fields}}
}

// NEDEN yardımcı fonksiyon: Content-Type ayarlama, status yazma ve JSON encode her yerde aynı;
// tekrar etmemek için tek fonksiyonda topladım.
// NEDEN sıralama önemli: Header'lar WriteHeader'dan ÖNCE ayarlanmalı, sonra ayarlanırsa etkisiz kalır.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // NEDEN hatayı yok sayıyorum: Status zaten gönderildi; bu noktada istemciye başka bir şey söylemenin yolu yok.
}
