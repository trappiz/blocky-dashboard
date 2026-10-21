BINARY  := blocky-dashboard
IMAGE   ?= blocky-dashboard:dev
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test run clean docker

build:
	go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o $(BINARY) ./cmd/blocky-dashboard

test:
	go test ./...

run:
	go run ./cmd/blocky-dashboard --config config.example.json

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

clean:
	rm -f $(BINARY)
