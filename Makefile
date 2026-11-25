SHELL := /bin/bash
TEST_DATABASE_URL ?= postgres://app:app@localhost:5433/campaigns_test?sslmode=disable

.PHONY: up down test test-go test-web test-local seed lint probe

up:
	docker compose up --build

down:
	docker compose down

# Go unit + integration (ayrı tmpfs test DB'si) + Vitest, hepsi container'da:
# host'ta yalnızca Docker gerekir. API key gerekmez; ilk çalıştırmada imaj ve
# bağımlılık indirmek için internet gerekir, sonrasında cache'ten çalışır.
test: test-go test-web

test-go:
	@# Önceki çalıştırmadan kalan test DB'si silinmiş bir ağa bağlı kalmış olabilir
	@# (araya `docker compose down` girdiyse). tmpfs'tir, silmek veri kaybettirmez.
	@docker compose --profile test rm -fsv db-test >/dev/null 2>&1 || true
	docker compose --profile test run --rm test-go

test-web:
	docker compose --profile test run --rm test-web

# Host'ta Go 1.27 + Node 24 varsa daha hızlı yol.
test-local:
	docker compose --profile test up -d --wait db-test
	cd backend && TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race -count=1 ./...
	cd frontend && npm ci --silent && npm run test

# Tüm uygulama verisini silip deterministik seed'i yeniden yazar.
seed:
	docker compose run --rm migrate /app/migrate -reseed

lint:
	cd backend && unformatted="$$(gofmt -l .)"; if [ -n "$$unformatted" ]; then echo "$$unformatted"; exit 1; fi
	cd backend && go vet ./...
	cd frontend && npm run typecheck

# Gerçek Claude ile structured output + streaming denemesi (ANTHROPIC_API_KEY gerekir).
probe:
	cd backend && go run ./cmd/claudeprobe -schema copy
	cd backend && go run ./cmd/claudeprobe -schema segment
	cd backend && go run ./cmd/claudeprobe -check-maxlength
