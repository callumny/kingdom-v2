.PHONY: fmt fmt-check vet test build check

fmt:
	go fmt ./...

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"

vet:
	go vet ./...

test:
	go test ./...

build:
	go build ./cmd/kingdom

check:
	$(MAKE) fmt && $(MAKE) fmt-check && $(MAKE) vet && $(MAKE) test && $(MAKE) build
