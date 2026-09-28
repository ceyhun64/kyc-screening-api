// Package config reads settings from environment variables.
//
// NEDEN ayrı paket: Ayarların nereden okunduğu tek bir yerde olsun. İleride bir
// ayar eklemek istersem sadece buraya bakarım.
package config

import (
	"errors"
	"os"
)

// Config holds the service settings.
// NEDEN struct: Ayarları tek tek global değişkenlerde tutmak yerine bir struct'ta topluyorum,
// böylece fonksiyonlara açıkça parametre olarak geçiyor ve test etmesi kolay.
type Config struct {
	Port        string
	DatabaseURL string
}

// Load reads the configuration from the environment.
func Load() (Config, error) {
	cfg := Config{
		Port:        getenv("PORT", "8080"), // NEDEN varsayılan değer: Port için makul bir varsayılan var, zorunlu tutmaya gerek yok.
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
	// NEDEN DATABASE_URL zorunlu: Veritabanı olmadan servis çalışamaz. Eksikse hemen,
	// açılışta ve anlaşılır bir mesajla hata vermek, ilk istekte gizemli bir hata almaktan iyidir ("fail fast").
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	return cfg, nil
}

// NEDEN küçük yardımcı fonksiyon: "değer varsa onu, yoksa varsayılanı kullan" mantığı
// her ayar için tekrarlanmasın.
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
