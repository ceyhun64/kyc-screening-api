// Package httpx contains small HTTP middlewares shared by all routes.
//
// NEDEN middleware: Loglama ve panic yakalama her endpoint'te gerekli. Her handler'a ayrı ayrı
// yazmak yerine tüm istekleri saran bir katman olarak bir kez yazıyorum. ASP.NET Core'daki
// middleware pipeline ile aynı fikir.
package httpx

import (
	"crypto/rand" // NEDEN crypto/rand (math/rand değil): Tahmin edilemez ID üretmek için güvenli rastgelelik.
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder remembers the status code written by the handler.
// NEDEN gerekli: http.ResponseWriter yazılan status kodunu sonradan okumaya izin vermiyor.
// ResponseWriter'ı sarıp WriteHeader çağrısını yakalayınca, log'a hangi kodun döndüğünü yazabiliyorum.
// NEDEN struct içinde http.ResponseWriter (embedding): Go'da kalıtım yok; gömme (embedding) ile
// ResponseWriter'ın tüm metodları otomatik geliyor, ben sadece WriteHeader'ı değiştiriyorum.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Logging logs one line per request with a request ID, so a single request
// can be followed through the logs. The ID is also returned to the client.
//
// NEDEN request ID: Bir kullanıcı "şu isteğim hata verdi" dediğinde, ID ile loglarda o isteği
// hemen bulabiliyorum (correlation ID). Mikroservislerde istek servisler arasında taşınırsa
// tüm servislerin loglarında aynı ID ile takip edilebilir.
func Logging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// NEDEN önce header'a bakıyorum: Önünde bir API gateway veya başka bir servis varsa
		// kendi ID'sini gönderebilir; aynı ID'yi kullanınca iz kopmuyor.
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)

		// NEDEN varsayılan 200: Handler WriteHeader'ı hiç çağırmadan doğrudan Write yaparsa Go otomatik 200 döner.
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		// NEDEN anahtar-değer çiftleri: Log sisteminde "status=500 olan istekler" veya
		// "duration_ms > 1000 olan istekler" gibi filtreleme yapılabiliyor.
		log.Info("request",
			"request_id", id,
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// Recover turns a panic in a handler into a 500 response instead of
// crashing the whole server.
//
// NEDEN: Go'da bir goroutine'deki yakalanmamış panic tüm programı çökertir. Aslında net/http her
// isteğin panic'ini kendisi yakalar, ama istemciye düzgün bir cevap dönmez, bağlantı kesilir.
// Bu middleware panic'i loglayıp istemciye tutarlı bir JSON 500 cevabı dönüyor.
// Normal hatalar için panic kullanmıyorum; bu sadece beklenmedik programcı hataları için bir güvenlik ağı.
func Recover(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// NEDEN defer içinde recover: recover() sadece defer edilmiş bir fonksiyon içinde çalışır.
		defer func() {
			if v := recover(); v != nil {
				log.Error("panic", "path", r.URL.Path, "panic", v)
				http.Error(w, `{"error":{"code":"internal_error","message":"something went wrong"}}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func newRequestID() string {
	b := make([]byte, 8) // NEDEN 8 bayt: 16 karakterlik hex ID; log takibi için yeterince benzersiz ve kısa.
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
