# NEDEN Makefile: Sık kullanılan komutlar kısa isimlerle çalışsın (make test, make run).
# Projeye yeni gelen biri hangi komutları çalıştıracağını burada görüyor.

DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/kyc?sslmode=disable
TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/kyc_test?sslmode=disable

.PHONY: run test test-integration lint up down

## run: start the API locally (needs PostgreSQL, e.g. `make up`)
run:
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/api

## test: unit tests with the race detector
test:
	go test -race ./...

## test-integration: unit + PostgreSQL integration tests
test-integration:
	docker compose exec -T db psql -U postgres -tc "SELECT 1 FROM pg_database WHERE datname = 'kyc_test'" | grep -q 1 || \
		docker compose exec -T db psql -U postgres -c "CREATE DATABASE kyc_test"
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race -count=1 ./...

## lint: formatting and static checks
lint:
	@test -z "$$(gofmt -l .)" || (echo "run gofmt on:"; gofmt -l .; exit 1)
	go vet ./...

## up / down: start or stop PostgreSQL and the API in Docker
up:
	docker compose up -d --build

down:
	docker compose down
