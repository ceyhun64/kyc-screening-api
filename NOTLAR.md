# Çalışma notları: kyc-screening-api

Bu klasör projenin **notlu kopyası**. Kodun her önemli yerinde `NEDEN:` ile başlayan Türkçe açıklamalar var. GitHub'daki sürüm temiz (İngilizce yorumlu) kalmalı; bu kopya sadece senin çalışman için.

## Okuma sırası

1. `internal/customer/customer.go`: Veri tipleri, hatalar ve doğrulama
2. `internal/customer/service.go`: İş kuralları ve interface'ler
3. `internal/customer/handler.go`: HTTP katmanı, hata → status kodu eşlemesi
4. `internal/customer/postgres.go`: SQL, transaction ve NULL işleme
5. `internal/screening/screener.go`: İsim normalizasyonu ve Levenshtein
6. `cmd/api/main.go`: Her şeyin bağlandığı yer ve graceful shutdown
7. `internal/database/database.go` ve `migrations/*.sql`
8. `internal/httpx/middleware.go`
9. Test dosyaları (`*_test.go`)
10. `Dockerfile`, `docker-compose.yml`, `.github/workflows/ci.yml`

## 30 saniyelik proje anlatımı (İngilizce)

> "To get hands-on with Go, I built a small REST service in the KYC domain. It onboards customers and screens their names against a sanctions list. Names are normalized first — Turkish characters, case, word order — and compared with Levenshtein similarity. A potential match never rejects the customer automatically; it moves them to manual review. Every screening is stored with the list version as an audit trail, and the screening and status change are saved in one transaction. It uses the standard library router, database/sql with PostgreSQL, table-driven tests, integration tests against a real database in CI, and a small distroless Docker image."

## CTO'nun sorabileceği sorular

| Soru | Kısa cevap | Dosya |
|---|---|---|
| Why did you define the interfaces in the service package? | Interfaces belong to the consumer in Go. The service doesn't depend on Postgres, and tests use a fake. | `service.go` |
| Why a transaction in SaveScreening? | Status and screening must be saved together, otherwise the audit trail is inconsistent. | `postgres.go` |
| What's the difference between errors.Is and errors.As? | Is checks for a specific error value in the chain; As checks for an error type and extracts it. | `handler.go` |
| Why not return the database error to the client? | It can leak internal details. Log it, return a generic 500. | `handler.go` |
| Why check the UUID format before querying? | Invalid IDs would cause a database error (500); the correct answer is 404. | `customer.go` |
| Why is `cs := []Customer{}` and not `var cs []Customer`? | A nil slice becomes `null` in JSON; clients expect `[]`. | `postgres.go` |
| Why `rows.Err()` after the loop? | `rows.Next()` returns false on error too; only `rows.Err()` tells you if something failed. | `postgres.go` |
| Why graceful shutdown? | In-flight requests finish during a deploy instead of being cut off. | `main.go` |
| Why did you use lib/pq instead of pgx? | Simple and dependency-free for a small project. In production I'd use pgx with sqlc. | `database.go` |
| Why not auto-reject a potential match? | Name similarity gives false positives. Compliance systems flag; humans decide. | `service.go` |
| What would you improve? | pgx + sqlc, golang-migrate, a real provider adapter with timeouts and retries, auth, OpenTelemetry. | `README.md` |
| How does Levenshtein work? | Minimum number of single-character edits (insert, delete, replace) to turn one string into another. | `screener.go` |

## Kendi bilgisayarında dene

```bash
docker compose up --build
curl -s -X POST localhost:8080/v1/customers -d '{"full_name":"Kemal Yildirimoglu","date_of_birth":"1975-06-01","country":"TR"}'
curl -s -X POST localhost:8080/v1/customers/<id>/screenings
```

Testleri çalıştır:

```bash
go test -race ./...
```

Kodda bir şeyi bilerek bozup (ör. `defer tx.Rollback()` satırını silip) testlerin ne dediğine bakmak, kodu anlamanın en hızlı yolu.
