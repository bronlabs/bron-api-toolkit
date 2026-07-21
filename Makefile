.PHONY: generate build test tidy

GO   := go
SPEC := bron-open-api-public.json

# Regenerate the catalog (catalog/{helpdoc.go,spec.json,spec.go}) from the spec.
# The catalog is the single generated artifact both consumers walk at runtime.
generate:
	$(GO) run ./cmd/cligen $(SPEC) catalog

build:
	$(GO) build ./...

test:
	$(GO) test ./...

tidy:
	$(GO) mod tidy
