// Command api starts the KYC screening HTTP service.
package main

import (
	"context"
	"errors"
	"log/slog"
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
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// ctx is cancelled on Ctrl+C or when Docker/Kubernetes sends SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := database.Migrate(ctx, db); err != nil {
		return err
	}

	// Wire dependencies by hand: no DI container needed.
	repo := customer.NewPostgresRepository(db)
	screener := screening.New(screening.DemoList, screening.DemoListVersion, 0.85)
	svc := customer.NewService(repo, screener)

	mux := http.NewServeMux()
	customer.NewHandler(svc, log).Register(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(pingCtx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok")) //nolint:errcheck
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpx.Recover(log, httpx.Logging(log, mux)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}

	// Give in-flight requests up to 10 seconds to finish.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
