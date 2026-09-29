export CGO_ENABLED := 0
export GOTOOLCHAIN := go$(shell cat .go-version)

.PHONY: check fmt test test-network build install-hooks coverage-update

check:
	@sh scripts/check.sh

fmt:
	@go fmt ./...

test:
	@go test ./...

test-network:
	@go test -count=1 -tags=network -run '^TestNetwork' ./internal/github

build:
	@go build -o bin/ferretta ./cmd/ferretta

coverage-update:
	@UPDATE_COVERAGE=1 sh scripts/check.sh

install-hooks:
	@git config core.hooksPath .githooks
