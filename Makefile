GOOSE = go run -tags='no_clickhouse,no_libsql,no_mssql,no_mysql,no_sqlite3,no_vertica,no_ydb' github.com/pressly/goose/v3/cmd/goose@v3.28.0

.PHONY: build run generate test test-race test-integration test-docker vet lint fmt migrate-up migrate-down migrate-status up down

build:
	go build -o bin/api ./cmd/api

run:
	go run ./cmd/api

generate:
	go generate ./...

test:
	go test -count=1 ./...

test-race:
	go test -race -count=1 ./...

test-integration:
	go test -tags=integration -count=1 ./...

test-docker:
	docker compose --profile test run --build --rm test

vet:
	go vet ./...

lint:
	golangci-lint run --build-tags=integration

fmt:
	gofmt -w cmd internal tests generate.go

migrate-up:
	cd migrations && $(GOOSE) -env=none -dir . -timeout 1m postgres "$$DATABASE_URL" up

# Destructive: rolls back the application schema in DATABASE_URL.
migrate-down:
	cd migrations && $(GOOSE) -env=none -dir . -timeout 1m postgres "$$DATABASE_URL" down

migrate-status:
	cd migrations && $(GOOSE) -env=none -dir . -timeout 1m postgres "$$DATABASE_URL" status

up:
	docker compose up --build -d

down:
	docker compose down
