.PHONY: build run generate test test-race test-integration test-docker bench vet lint fmt migrate-up migrate-down migrate-status up down

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

bench:
	go test ./internal/graph/benchmarks -run='^$$' -bench='Benchmark(Factory|Decode)' -benchmem -benchtime=200ms -count=10 -cpu=1

vet:
	go vet ./...

lint:
	golangci-lint run --build-tags=integration

fmt:
	gofmt -w cmd internal tests generate.go

migrate-up:
	docker compose run --rm migrate up

# Destructive: rolls back the application schema in the Compose database.
migrate-down:
	docker compose run --rm migrate down

migrate-status:
	docker compose run --rm migrate status

up:
	docker compose up --build -d

down:
	docker compose down
