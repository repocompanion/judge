BINARY := companion-judge

.PHONY: build test vet fmt clean

build:
	go build -trimpath -o $(BINARY) ./cmd/companion-judge

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

clean:
	rm -f $(BINARY)
	rm -rf dist
