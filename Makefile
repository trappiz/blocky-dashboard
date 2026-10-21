BINARY := blocky-dashboard

.PHONY: build test run clean

build:
	go build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/blocky-dashboard

test:
	go test ./...

run:
	go run ./cmd/blocky-dashboard --config config.example.json

clean:
	rm -f $(BINARY)
