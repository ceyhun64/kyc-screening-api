// Package customer contains the customer onboarding and screening feature:
// domain types, business logic, the HTTP handlers and the PostgreSQL
// repository. Everything for one feature lives in one package.
//
// NEDEN "özelliğe göre paket" (package by feature): .NET'te sık görülen
// Controllers/, Services/, Repositories/ gibi katman klasörleri yerine, bir
// özelliğe ait her şeyi tek pakette topladım. Go topluluğunda daha yaygın olan
// yaklaşım bu. Katmanlar yine ayrı dosyalarda: handler.go, service.go, postgres.go.
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
// NEDEN string yerine kendi tipim: Status'u düz string yapsaydım, fonksiyonlara
// yanlışlıkla herhangi bir metin geçebilirdim. Ayrı tip, kodun niyetini açıkça gösteriyor.
type Status string

// NEDEN sabitler (const): "review" gibi değerleri koda her yerde elle yazmak yerine
// tek yerde tanımlıyorum; yazım hatası derleme anında yakalanır.
const (
	StatusPending Status = "pending" // not screened yet
	StatusClear   Status = "clear"   // screened, no match found
	StatusReview  Status = "review"  // potential match, needs a human to review
)

// Customer is a person being onboarded.
// NEDEN json tag'leri: Go'da alan adları büyük harfle başlar (FullName), ama API'de
// snake_case (full_name) kullanmak istiyorum. Tag'ler JSON'daki adı belirliyor.
type Customer struct {
	ID          string    `json:"id"`
	FullName    string    `json:"full_name"`
	DateOfBirth string    `json:"date_of_birth"` // YYYY-MM-DD — NEDEN string: Doğum tarihinin saati/saat dilimi yok; time.Time kullansam JSON'da gereksiz "T00:00:00Z" çıkardı.
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
//
// NEDEN taramalar hiç güncellenmiyor/silinmiyor: AML'de "bu müşteriye neden onay
// verildi?" sorusuna geriye dönük cevap verebilmek gerekir. Her tarama ayrı bir kayıt
// olarak saklanınca tam bir denetim izi (audit trail) oluşuyor.
type Screening struct {
	ID          string          `json:"id"`
	CustomerID  string          `json:"customer_id"`
	Result      ScreeningResult `json:"result"`
	MatchedName string          `json:"matched_name,omitempty"` // NEDEN omitempty: Eşleşme yoksa alan boş; JSON'da hiç görünmesin.
	Score       float64         `json:"score"`
	ListVersion string          `json:"list_version"` // NEDEN liste versiyonu: Liste zamanla değişir. Hangi kararın hangi listeye göre verildiğini bilmek gerekir.
	CreatedAt   time.Time       `json:"created_at"`
}

// CreateInput is the data needed to create a customer.
// NEDEN Customer'dan ayrı bir tip: İstemci ID, status veya created_at gönderemesin.
// Bunları sistem belirliyor. Girdi tipi sadece kullanıcının göndermesi gereken alanları içeriyor.
type CreateInput struct {
	FullName    string `json:"full_name"`
	DateOfBirth string `json:"date_of_birth"`
	Country     string `json:"country"`
}

// ErrNotFound is returned when a customer does not exist.
// NEDEN "sentinel error" (paket seviyesinde sabit hata): Katmanlar arasında
// errors.Is(err, ErrNotFound) ile kontrol edilebiliyor. Handler bunu görünce 404 dönüyor.
// Veritabanına özel sql.ErrNoRows'un handler'a kadar sızmasını da engelliyor.
var ErrNotFound = errors.New("not found")

// ValidationError lists the invalid fields and why they are invalid.
// NEDEN özel hata tipi: Sadece "hatalı istek" demek yerine hangi alanın neden hatalı
// olduğunu istemciye söyleyebilmek için. Handler errors.As ile bu tipi yakalayıp 400 dönüyor.
type ValidationError struct {
	Fields map[string]string
}

// NEDEN Error() metodu: Go'da bir tipin "error" sayılması için bu tek metod yeterli
// (interface'ler örtük olarak sağlanır, "implements" yazmaya gerek yok).
func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	// NEDEN sıralama: Go'da map üzerinde dönme sırası rastgele. Sıralamazsam aynı hata
	// mesajı her seferinde farklı sırada çıkardı; loglar ve testler kararsız olurdu.
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", k, e.Fields[k]))
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// NEDEN regex'i paket seviyesinde bir kez derliyorum: MustCompile her istekte
// çalışsaydı gereksiz maliyet olurdu. Bir kez derlenip tekrar tekrar kullanılıyor.
var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)

// normalize cleans up the input before validation.
// NEDEN önce temizle, sonra doğrula: "  mt " gibi bir girdi aslında geçerli ("MT").
// Kullanıcıyı gereksiz yere reddetmemek için önce boşlukları ve harf büyüklüğünü düzeltiyorum.
func (in CreateInput) normalize() CreateInput {
	return CreateInput{
		FullName:    strings.Join(strings.Fields(in.FullName), " "), // NEDEN Fields+Join: Baştaki/sondaki ve aradaki fazla boşlukları tek boşluğa indiriyor.
		DateOfBirth: strings.TrimSpace(in.DateOfBirth),
		Country:     strings.ToUpper(strings.TrimSpace(in.Country)),
	}
}

// validate checks the input. now is passed in so tests don't depend on the clock.
// NEDEN "now" parametre olarak geliyor: time.Now() doğrudan çağırsaydım, "tarih gelecekte
// olamaz" testi yıllar sonra farklı sonuç verebilirdi. Zamanı dışarıdan verince test sabit kalıyor.
func (in CreateInput) validate(now time.Time) error {
	// NEDEN tüm hataları topluyorum: İlk hatada durup dönseydim, kullanıcı hataları tek tek
	// düzeltip tekrar denemek zorunda kalırdı. Hepsini bir kerede göstermek daha iyi bir API deneyimi.
	fields := map[string]string{}

	// NEDEN []rune ile uzunluk: len(string) bayt sayar. "Ayşe" 4 harf ama 5 bayt ("ş" 2 bayt).
	// Rune'a çevirince gerçek karakter sayısını alıyorum; Türkçe isimler için önemli.
	if n := len([]rune(in.FullName)); n < 2 || n > 200 {
		fields["full_name"] = "must be between 2 and 200 characters"
	}

	// NEDEN "2006-01-02": Go'nun tarih formatı referans tarihle yazılır (2006-01-02 15:04:05).
	// Bu, YYYY-MM-DD anlamına geliyor.
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
//
// NEDEN: PostgreSQL "abc" gibi bir değeri uuid kolonunda aramaya çalışınca hata verir.
// Bu kontrol olmasa kullanıcı 500 (sunucu hatası) görürdü. Oysa doğru cevap 404:
// "böyle bir müşteri yok". Ayrıca veritabanına gereksiz sorgu gitmemiş oluyor.
func validID(id string) bool {
	return uuidPattern.MatchString(id)
}
