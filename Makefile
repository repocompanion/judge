BINARY := companion-judge
COMPANION ?= companion

.PHONY: build test vet fmt clean smoke

build:
	go build -trimpath -o $(BINARY) ./cmd/companion-judge

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

smoke:
	go run ./cmd/companion-judge review \
		--store testdata/reviews/sqs-body-logging/docs \
		--project acme/api \
		--lang go \
		--diff testdata/reviews/sqs-body-logging/change.patch \
		--title "add SQS consumer" \
		--companion "$(COMPANION)" \
		--dry-run

clean:
	rm -f $(BINARY)
	rm -rf dist
