.PHONY: build test test-race vet fmt run docker-up docker-down

build:
	go build ./...

test:
	go test -count=1 ./...

test-race:
	go test -race -count=1 ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

run:
	go run ./cmd/api

docker-up:
	docker compose up --build

docker-down:
	docker compose down
