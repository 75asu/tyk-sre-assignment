# Makefile , format/vet/test/build the Go tool, plus a self-contained kind + Helm demo.
# The Go module is in ./golang; the Helm chart is in ./helm; the consumer deploy is in ./sample-deploy.

GODIR           := golang
BINARY          := tyk-sre-assignment
IMAGE           := tyk-sre-assignment
CHART           := helm/tyk-sre-assignment
RELEASE         := tyk-sre-assignment
NAMESPACE       := tyk-sre
KIND_CLUSTER    := tyk-sre
KIND_KUBECONFIG ?= $(CURDIR)/.kube/kind.kubeconfig

.DEFAULT_GOAL := help
.PHONY: help fmt fmt-check vet test build image helm-lint helm-template \
        kind-up kind-down deploy undeploy seed demo e2e

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

# --- consumer deploy on kind: pulls chart + image from GHCR (needs Docker for kind + helmfile) ---

kind-up: ## Create an isolated kind cluster (own kubeconfig)
	@mkdir -p $(dir $(KIND_KUBECONFIG))
	kind create cluster --name $(KIND_CLUSTER) --kubeconfig "$(KIND_KUBECONFIG)"

deploy: ## Deploy by pulling the chart + image from remote GHCR (the consumer path; needs helmfile)
	KUBECONFIG="$(KIND_KUBECONFIG)" helmfile -f sample-deploy/helmfile.yaml sync

seed: ## Apply the sample workloads (a healthy + a degraded Deployment) to exercise the tool
	kubectl --kubeconfig "$(KIND_KUBECONFIG)" apply \
		-f sample-deploy/namespace.yaml -f sample-deploy/web-healthy.yaml -f sample-deploy/web-degraded.yaml

undeploy: ## Uninstall the release
	KUBECONFIG="$(KIND_KUBECONFIG)" helmfile -f sample-deploy/helmfile.yaml destroy

demo: ## Curl every endpoint + show the tool create/remove a NetworkPolicy (run after deploy + seed)
	@kubectl --kubeconfig "$(KIND_KUBECONFIG)" -n $(NAMESPACE) port-forward svc/$(RELEASE) 8080:8080 >/dev/null 2>&1 & \
	pf=$$!; trap 'kill $$pf 2>/dev/null' EXIT; sleep 3; \
	iso='{"a":{"namespace":"demo","labels":{"app.kubernetes.io/name":"web-healthy"}},"b":{"namespace":"demo","labels":{"app.kubernetes.io/name":"web-degraded"}}}'; \
	echo "== GET /healthz =="; curl -s localhost:8080/healthz; echo; echo; \
	echo "== GET /readyz =="; curl -s localhost:8080/readyz | (jq . 2>/dev/null || cat); echo; \
	echo "== GET /deployments/unhealthy =="; curl -s localhost:8080/deployments/unhealthy | (jq . 2>/dev/null || cat); echo; \
	echo "== POST /network-policies/isolate =="; curl -s -X POST -H 'Content-Type: application/json' -d "$$iso" localhost:8080/network-policies/isolate | (jq . 2>/dev/null || cat); echo; \
	echo "-- NetworkPolicies the tool created (enforced by a policy-aware CNI e.g. Calico) --"; kubectl --kubeconfig "$(KIND_KUBECONFIG)" -n demo get networkpolicy; echo; \
	echo "== DELETE /network-policies/isolate =="; curl -s -X DELETE -H 'Content-Type: application/json' -d "$$iso" localhost:8080/network-policies/isolate | (jq . 2>/dev/null || cat); echo; \
	echo "-- NetworkPolicies after de-isolate --"; kubectl --kubeconfig "$(KIND_KUBECONFIG)" -n demo get networkpolicy

e2e: ## Full local run: fresh kind -> pull chart + image from GHCR -> seed -> demo (curl all endpoints)
	-$(MAKE) kind-down
	$(MAKE) kind-up
	$(MAKE) deploy
	$(MAKE) seed
	$(MAKE) demo

kind-down: ## Delete the kind cluster + its kubeconfig
	-kind delete cluster --name $(KIND_CLUSTER)
	rm -f "$(KIND_KUBECONFIG)"
