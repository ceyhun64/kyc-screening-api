// Command api starts the KYC screening HTTP service.
//
// NEDEN cmd/api klasörü: Go projelerinde çalıştırılabilir programlar (main
// paketleri) genelde cmd/<isim>/ altında durur. İleride bir CLI aracı veya
// worker eklersem cmd/worker/ gibi yan yana durabilir.
package main

import (
	"context"
	"errors"
	"log/slog" // NEDEN: Go 1.21'den beri standart kütüphanede yapılandırılmış (structured) log var, harici kütüphaneye gerek yok.
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ceyhun64/kyc-screening-api/internal/config"
	"github.com/ceyhun64/kyc-screening-api/internal/customer"
	"github.com/ceyhun64/kyc-screening-api/internal/database"
	"github.com/ceyhun64/kyc-screening-api/internal/httpx"
	"github.com/ceyhun64/kyc-screening-api/internal/screening"
)

func main() {
	// NEDEN JSON log: Loglar Docker/cloud ortamında bir log sistemine gider.
	// JSON formatı makinenin okuyup filtrelemesini kolaylaştırır (ör. status=500 olanları bul).
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// NEDEN main'i kısa tutup run() çağırıyorum: run() hata döndürebiliyor.
	// Böylece tüm hatalar tek yerde loglanıp os.Exit(1) ile çıkılıyor.
	// Ayrıca os.Exit defer'ları çalıştırmaz; run() içindeki defer'lar (db.Close gibi)
	// run bitince düzgün çalışmış oluyor.
	if err := run(log); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	// NEDEN ayarları environment variable'dan okuyorum: 12-factor app prensibi.
	// Aynı Docker imajı farklı ortamlarda (dev, test, prod) sadece env değişerek çalışır,
	// şifreler koda gömülmez.
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// NEDEN signal.NotifyContext: Ctrl+C veya Docker/Kubernetes'in gönderdiği SIGTERM
	// geldiğinde ctx iptal olur. Bunu aşağıda graceful shutdown için kullanıyorum.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close() // NEDEN defer: fonksiyon nasıl biterse bitsin bağlantı havuzu kapansın.

	// NEDEN migration'ı açılışta çalıştırıyorum: Küçük bir projede en basit yol.
	// Büyük sistemlerde migration genelde ayrı bir CI/CD adımı olur (README'de not ettim).
	if err := database.Migrate(ctx, db); err != nil {
		return err
	}

	// NEDEN bağımlılıkları elle bağlıyorum (manual dependency injection):
	// Go'da .NET'teki gibi bir DI container kullanmak yaygın değil.
	// Constructor'lar interface alıyor, hepsi burada açıkça birbirine bağlanıyor.
	// Kodu okuyan biri hangi parçanın neye bağlı olduğunu tek bakışta görüyor.
	repo := customer.NewPostgresRepository(db)
	screener := screening.New(screening.DemoList, screening.DemoListVersion, 0.85) // NEDEN 0.85: %85 ve üstü benzerlik "olası eşleşme" sayılıyor. Bir kural, gerçekte iş birimiyle belirlenir.
	svc := customer.NewService(repo, screener)

	// NEDEN standart ServeMux: Go 1.22'den beri "GET /v1/customers/{id}" gibi
	// method + path parametresi destekliyor. Gin/Chi gibi bir router'a ihtiyaç kalmadı.
	mux := http.NewServeMux()
	customer.NewHandler(svc, log).Register(mux)

	// NEDEN healthz veritabanını da kontrol ediyor: Load balancer veya Kubernetes bu
	// endpoint'e bakarak servisin gerçekten iş yapabilir durumda olup olmadığına karar verir.
	// DB yoksa servis "ayakta" ama işe yaramaz; 503 dönmek bunu doğru anlatır.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second) // NEDEN timeout: DB takılırsa health check de sonsuza kadar beklemesin.
		defer cancel()
		if err := db.PingContext(pingCtx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok")) //nolint:errcheck
	})

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		// NEDEN middleware'leri iç içe sarıyorum: İstek önce Recover'dan, sonra Logging'den,
		// en son asıl handler'dan geçiyor. Recover en dışta ki Logging'de bile panic olsa yakalasın.
		Handler: httpx.Recover(log, httpx.Logging(log, mux)),
		// NEDEN timeout'lar: Varsayılan http.Server'da timeout yok. Çok yavaş gönderen
		// kötü niyetli istemciler (Slowloris saldırısı) bağlantıları sonsuza kadar açık tutabilir.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// NEDEN sunucuyu goroutine'de başlatıyorum: ListenAndServe bloklar (dönmez).
	// Ayrı goroutine'de çalışınca ana akış aşağıda hem sunucu hatasını hem kapatma sinyalini bekleyebiliyor.
	// NEDEN buffered channel (kapasite 1): Kimse okumasa bile goroutine yazıp çıkabilsin, sızıntı olmasın.
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	// NEDEN select: İki olaydan hangisi önce olursa: sunucu hata verip durdu, ya da kapatma sinyali geldi.
	select {
	case err := <-errCh:
		// NEDEN ErrServerClosed kontrolü: Shutdown çağrılınca ListenAndServe bu hatayı döner, bu normal bir durum.
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}

	// NEDEN graceful shutdown: Sunucu yeni istek almayı bırakıyor ama işlemi süren istekleri
	// 10 saniyeye kadar bitirmelerine izin veriyor. Deploy sırasında yarım kalan istek (ör. yarım
	// kaydedilmiş bir tarama) olmuyor.
	// NEDEN context.Background(): ctx zaten iptal oldu; onu kullansam Shutdown hiç beklemezdi.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
