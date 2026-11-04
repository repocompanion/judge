BINARY := judge
COMPANION ?= companion

.PHONY: build test vet fmt fmt-check check clean smoke

build:
	go build -trimpath -o $(BINARY) ./cmd/judge

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

fmt-check:
	@files="$$(gofmt -l cmd internal)"; \
	if [ -n "$$files" ]; then \
		echo "unformatted files:"; \
		echo "$$files"; \
		exit 1; \
	fi

check: fmt-check vet test

smoke:
	go run ./cmd/judge review \
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
