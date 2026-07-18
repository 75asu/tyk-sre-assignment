# Makefile , format/vet/test/build the Go tool, plus a self-contained kind + Helm demo.
# The Go module is in ./golang; the Helm chart is in ./helm.

GODIR           := golang
BINARY          := tyk-sre-assignment
IMAGE           := tyk-sre-assignment
CHART           := helm/tyk-sre-assignment
RELEASE         := tyk-sre-assignment
NAMESPACE       := tyk-sre
KIND_CLUSTER    := tyk-sre
KIND_KUBECONFIG := $(CURDIR)/.kube/kind.kubeconfig

.DEFAULT_GOAL := help
.PHONY: help fmt fmt-check vet test build image helm-lint helm-template \
        kind-up kind-down kind-load deploy undeploy demo

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-13s\033[0m %s\n", $$1, $$2}'

# --- code ---

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

# --- image + chart ---

image: ## Build the container image locally (tag :local)
	docker build -t $(IMAGE):local .

helm-lint: ## Lint the Helm chart
	helm lint $(CHART)

helm-template: ## Render the chart to stdout
	helm template $(RELEASE) $(CHART)

# --- self-contained kind + Helm demo (needs OrbStack/Docker) ---

kind-up: ## Create an isolated kind cluster (own kubeconfig)
	@mkdir -p $(dir $(KIND_KUBECONFIG))
	kind create cluster --name $(KIND_CLUSTER) --kubeconfig "$(KIND_KUBECONFIG)"

kind-load: image ## Build + load the local image into kind (no registry needed)
	kind load docker-image $(IMAGE):local --name $(KIND_CLUSTER)

deploy: kind-load ## Install/upgrade the chart into kind using the local image
	helm upgrade --install $(RELEASE) $(CHART) \
		--kubeconfig "$(KIND_KUBECONFIG)" \
		--namespace $(NAMESPACE) --create-namespace \
		--set image.repository=$(IMAGE) --set image.tag=local --set image.pullPolicy=Never \
		--wait

undeploy: ## Uninstall the chart
	helm uninstall $(RELEASE) --kubeconfig "$(KIND_KUBECONFIG)" --namespace $(NAMESPACE)

demo: ## Port-forward the Service and curl the endpoints (run after 'make deploy')
	@kubectl --kubeconfig "$(KIND_KUBECONFIG)" -n $(NAMESPACE) port-forward svc/$(RELEASE) 8080:8080 >/dev/null 2>&1 & \
	pf=$$!; sleep 3; \
	echo "== /healthz =="; curl -s localhost:8080/healthz; echo; \
	echo "== /readyz =="; curl -s localhost:8080/readyz | (jq . 2>/dev/null || cat); echo; \
	echo "== /deployments/unhealthy =="; curl -s localhost:8080/deployments/unhealthy | (jq . 2>/dev/null || cat); echo; \
	kill $$pf 2>/dev/null

kind-down: ## Delete the kind cluster + its kubeconfig
	-kind delete cluster --name $(KIND_CLUSTER)
	rm -f "$(KIND_KUBECONFIG)"
