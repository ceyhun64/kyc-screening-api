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
//
// NEDEN interface burada (kullanıldığı yerde) tanımlı: Go'da yaygın kural "interface'i
// kullanan taraf tanımlar". .NET'te IRepository genelde implementasyonun yanında durur.
// Go'da interface'ler örtük (implicit) sağlandığı için PostgresRepository bu interface'i
// hiç bilmeden uygulamış oluyor. Sonuç: service PostgreSQL'e bağımlı değil, testte
// sahte (fake) bir repository kolayca verilebiliyor.
type Repository interface {
	// NEDEN her metodun ilk parametresi context: İstek iptal olursa veya zaman aşımı olursa
	// veritabanı sorgusu da iptal edilebilsin. Go'da I/O yapan fonksiyonların standart imzası bu.
	Create(ctx context.Context, in CreateInput) (Customer, error)
	Get(ctx context.Context, id string) (Customer, error)
	List(ctx context.Context, limit, offset int) ([]Customer, error)
	// SaveScreening stores the screening and updates the customer's status
	// in one transaction.
	SaveScreening(ctx context.Context, s Screening, newStatus Status) (Screening, error)
	ListScreenings(ctx context.Context, customerID string) ([]Screening, error)
}

// Screener checks a name against a sanctions list.
// NEDEN interface: Bugün hafızadaki demo liste var. Yarın gerçek bir tarama sağlayıcısının
// API'si gelse, aynı Screen metodunu uygulayan bir adapter yazmam yeterli. Service değişmez.
// Küçük interface (tek metod) Go'da tercih edilen tarz.
type Screener interface {
	Screen(name string) screening.Result
}

// Service holds the business logic.
// NEDEN alanlar küçük harfle başlıyor: Paket dışından erişilemesin (private).
// Service sadece NewService ile oluşturulabiliyor, böylece yarım yapılandırılmış olamıyor.
type Service struct {
	repo     Repository
	screener Screener
	now      func() time.Time // NEDEN fonksiyon olarak saat: Testlerde sabit bir zaman verebilmek için.
}

// NewService wires the service with its dependencies.
// NEDEN constructor fonksiyon: Go'da constructor yok; New... fonksiyonları bu işi görüyor.
// Bağımlılıklar parametre olarak geliyor (dependency injection, container olmadan).
func NewService(repo Repository, screener Screener) *Service {
	return &Service{repo: repo, screener: screener, now: time.Now}
}

// NEDEN limit sınırları: İstemci limit=1000000 gönderip tüm tabloyu tek istekte çekmesin.
// Veritabanını ve belleği korumak için bir üst sınır koyuyorum.
const (
	defaultLimit = 20
	maxLimit     = 100
)

// CreateCustomer validates the input and stores a new customer.
func (s *Service) CreateCustomer(ctx context.Context, in CreateInput) (Customer, error) {
	in = in.normalize()
	// NEDEN doğrulama service'te (handler'da değil): İş kuralları HTTP'ye bağlı olmasın.
	// İleride aynı servisi bir kuyruk tüketicisi veya CLI çağırsa da aynı kurallar geçerli olur.
	if err := in.validate(s.now()); err != nil {
		return Customer{}, err
	}

	c, err := s.repo.Create(ctx, in)
	if err != nil {
		// NEDEN %w ile sarmalama (wrap): Hataya "nerede oldu" bilgisi ekleniyor
		// ("create customer: insert customer: ..."), ama orijinal hata kaybolmuyor.
		// Üst katman hâlâ errors.Is ile orijinal hatayı kontrol edebiliyor.
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
	// NEDEN hata dönmek yerine değerleri düzeltiyorum: Sayfalama parametreleri için
	// makul varsayılanlara çekmek kullanıcı dostu. limit=0 → 20, limit=500 → 100, offset=-5 → 0.
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
	// NEDEN önce müşteriyi çekiyorum: Hem müşterinin var olduğunu doğruluyorum (yoksa 404),
	// hem de taranacak ismi alıyorum.
	c, err := s.GetCustomer(ctx, id)
	if err != nil {
		return Screening{}, fmt.Errorf("screen customer: %w", err)
	}

	r := s.screener.Screen(c.FullName)

	// NEDEN varsayılan "clear" ile başlıyorum: Eşleşme yoksa en yaygın durum bu;
	// sadece eşleşme varsa değerleri değiştiriyorum. Kod daha kısa ve okunaklı.
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
		//
		// NEDEN otomatik "reddedildi" demiyorum: İsim benzerliği yanlış pozitif verebilir
		// (aynı isimli farklı biri). Compliance sistemlerinde olası eşleşmeler bir uzmanın
		// incelemesine (manual review) gider. Sistem karar vermez, işaretler.
		sc.Result = ResultPotentialMatch
		sc.MatchedName = r.MatchedName
		status = StatusReview
	}

	// NEDEN tarama ve durum tek metotta kaydediliyor: İkisi birlikte başarılı ya da
	// birlikte başarısız olmalı. Repository bunu bir transaction içinde yapıyor.
	saved, err := s.repo.SaveScreening(ctx, sc, status)
	if err != nil {
		return Screening{}, fmt.Errorf("save screening for %s: %w", id, err)
	}
	return saved, nil
}

// ListScreenings returns the screening history of a customer, newest first.
func (s *Service) ListScreenings(ctx context.Context, id string) ([]Screening, error) {
	// NEDEN önce müşteri kontrolü: Müşteri yoksa boş liste ([]) değil 404 dönmek doğru.
	// Boş liste "müşteri var ama hiç taranmamış" anlamına gelir; ikisini karıştırmamak gerekiyor.
	if _, err := s.GetCustomer(ctx, id); err != nil {
		return nil, err
	}
	list, err := s.repo.ListScreenings(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list screenings for %s: %w", id, err)
	}
	return list, nil
}
