// Package database opens the PostgreSQL connection and applies migrations.
package database

import (
	"context"
	"database/sql"
	"embed" // NEDEN embed: SQL dosyalarını derleme sırasında binary'nin içine gömüyor.
	"fmt"
	"io/fs"
	"sort"
	"time"

	// NEDEN alt çizgi (_) ile import: Bu paketin fonksiyonlarını doğrudan kullanmıyorum.
	// Import edilince kendini "postgres" sürücüsü olarak database/sql'e kaydediyor.
	// NEDEN lib/pq: Bağımlılığı olmayan, olgun bir PostgreSQL sürücüsü. Daha büyük bir
	// projede pgx'i tercih ederdim (daha hızlı, daha fazla özellik); README'de not ettim.
	_ "github.com/lib/pq" // registers the "postgres" driver
)

// NEDEN migration'lar binary'ye gömülü: Docker imajında sadece tek bir binary var
// (distroless imaj). SQL dosyalarını ayrıca kopyalamaya gerek kalmıyor; kod ve şema
// her zaman aynı versiyonda geliyor.
//
//go:embed migrations/*.sql
var migrations embed.FS

// Open connects to PostgreSQL and waits until it answers, which helps when
// the database container is still starting (e.g. with docker compose).
func Open(ctx context.Context, url string) (*sql.DB, error) {
	// NEDEN sql.Open bağlanmıyor: sql.Open sadece yapılandırmayı hazırlar, gerçek bağlantı
	// ilk sorguda açılır. Bu yüzden aşağıda Ping ile gerçekten bağlanabildiğimi kontrol ediyorum.
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// NEDEN havuz ayarları: Varsayılan olarak açık bağlantı sayısı sınırsız. Yoğun trafikte
	// PostgreSQL'in bağlantı limitini doldurabilir. Sınır koyup bağlantıları belli süre sonra yeniliyorum.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	// NEDEN tekrar deneme (retry): docker compose'da uygulama ve veritabanı aynı anda başlar.
	// Veritabanı birkaç saniye geç hazır olabilir. Hemen çökmek yerine 10 kez, 1 saniye arayla deniyorum.
	for attempt := 1; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err = db.PingContext(pingCtx)
		cancel() // NEDEN defer değil de hemen cancel: Döngü içinde defer kullanılırsa fonksiyon bitene kadar birikir.
		if err == nil {
			return db, nil
		}
		if attempt == 10 {
			db.Close()
			return nil, fmt.Errorf("ping database after %d attempts: %w", attempt, err)
		}
		// NEDEN select ile bekliyorum: time.Sleep kullansaydım Ctrl+C geldiğinde bile beklemeye
		// devam ederdi. select sayesinde kapatma sinyali gelince hemen çıkıyor.
		select {
		case <-ctx.Done():
			db.Close()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// Migrate applies the SQL files in migrations/ in name order. Applied files
// are recorded in schema_migrations, so each file runs only once.
//
// This is intentionally minimal. A production service would use a tool such
// as golang-migrate or goose, and take a lock so that two instances starting
// at the same time don't migrate in parallel.
//
// NEDEN kendi basit migration'ım: Projeyi küçük tutmak ve mekanizmayı anladığımı göstermek için.
// Mantık, EF Core'un __EFMigrationsHistory tablosuyla aynı: hangi dosyanın uygulandığı bir
// tabloda tutuluyor, her dosya yalnızca bir kez çalışıyor.
func Migrate(ctx context.Context, db *sql.DB) error {
	// NEDEN IF NOT EXISTS: Servis her açılışta bunu çalıştırıyor; tablo zaten varsa hata vermesin (idempotent).
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	// NEDEN sıralama: Migration'lar sırayla uygulanmalı (001, 002, ...). Dosya adındaki numara bunu sağlıyor.
	sort.Strings(files)

	for _, file := range files {
		if err := applyOne(ctx, db, file); err != nil {
			return fmt.Errorf("migration %s: %w", file, err)
		}
	}
	return nil
}

func applyOne(ctx context.Context, db *sql.DB, file string) error {
	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, file,
	).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil // NEDEN: Bu dosya daha önce uygulanmış, tekrar çalıştırmıyorum.
	}

	content, err := migrations.ReadFile(file)
	if err != nil {
		return err
	}

	// NEDEN her migration bir transaction içinde: PostgreSQL'de şema değişiklikleri (CREATE TABLE vb.)
	// transaction içinde çalışabiliyor. Dosyanın yarısı uygulanıp yarısı hata verirse hepsi geri alınıyor,
	// veritabanı yarım kalmış bir şemayla kalmıyor.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		return err
	}
	// NEDEN kaydı aynı transaction'da ekliyorum: Migration uygulanıp kaydı yazılamazsa, bir sonraki
	// açılışta tekrar uygulanmaya çalışılır ve hata verir. İkisi birlikte olmalı.
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, file); err != nil {
		return err
	}
	return tx.Commit()
}
