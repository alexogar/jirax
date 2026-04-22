APP := jirax

.PHONY: build run test fmt clean

build:
	go build -o $(APP) ./cmd/jirax

run:
	go run ./cmd/jirax --help

test:
	go test ./...

fmt:
	gofmt -w ./cmd/jirax ./internal/jirax

clean:
	go clean
