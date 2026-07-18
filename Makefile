# Makefile , format, vet, test and build the SRE tool. The Go module is in ./golang.

GODIR  := golang
BINARY := tyk-sre-assignment

.DEFAULT_GOAL := help
.PHONY: help fmt fmt-check vet test build

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-11s\033[0m %s\n", $$1, $$2}'

fmt: ## Format the code (gofmt, in place)
	cd $(GODIR) && go fmt ./...

fmt-check: ## Check formatting without modifying files (used by CI)
	@unformatted="$$(cd $(GODIR) && gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt:"; echo "$$unformatted"; exit 1; \
	fi; \
	echo "gofmt: clean"

vet: ## Static checks (go vet)
	cd $(GODIR) && go vet ./...

test: ## Run unit tests with the race detector + coverage
	cd $(GODIR) && go test -race -cover ./...

build: ## Build the binary into golang/bin
	cd $(GODIR) && go build -o bin/$(BINARY) .
