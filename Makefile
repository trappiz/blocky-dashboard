BINARY := blocky-dashboard

.PHONY: build test run clean

build:
	go build -trimpath -o $(BINARY) main.go

test:
	go test ./...

run:
	go run blocky-dashboard --config config.example.json

clean:
	rm -f $(BINARY)
