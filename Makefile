# Makefile , format/vet/test/build the Go tool, plus a self-contained kind + Helm demo.
# The Go module is in ./golang; the Helm chart is in ./helm.

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
        kind-up kind-down deploy undeploy seed demo e2e e2e-verify

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

demo: ## Port-forward the Service and curl the endpoints (run after 'make deploy')
	@kubectl --kubeconfig "$(KIND_KUBECONFIG)" -n $(NAMESPACE) port-forward svc/$(RELEASE) 8080:8080 >/dev/null 2>&1 & \
	pf=$$!; sleep 3; \
	echo "== /healthz =="; curl -s localhost:8080/healthz; echo; \
	echo "== /readyz =="; curl -s localhost:8080/readyz | (jq . 2>/dev/null || cat); echo; \
	echo "== /deployments/unhealthy =="; curl -s localhost:8080/deployments/unhealthy | (jq . 2>/dev/null || cat); echo; \
	kill $$pf 2>/dev/null

e2e: ## Consumer e2e: fresh kind -> pull chart + image from GHCR -> seed -> assert (local, CI, or 3rd party)
	-$(MAKE) kind-down
	$(MAKE) kind-up
	$(MAKE) deploy
	$(MAKE) seed
	$(MAKE) e2e-verify

e2e-verify: ## Assert all three SRE features against the running tool (readiness + health + isolation); reused by CI
	@echo "waiting for web-healthy to become Ready (a healthy Deployment must drop OFF the list)..."
	kubectl --kubeconfig "$(KIND_KUBECONFIG)" -n demo rollout status deploy/web-healthy --timeout=120s
	@kubectl --kubeconfig "$(KIND_KUBECONFIG)" -n $(NAMESPACE) port-forward svc/$(RELEASE) 8080:8080 >/dev/null 2>&1 & \
	pf=$$!; trap 'kill $$pf 2>/dev/null' EXIT; sleep 3; \
	npsel="app.kubernetes.io/managed-by=tyk-sre-assignment"; \
	iso='{"a":{"namespace":"demo","labels":{"app.kubernetes.io/name":"web-healthy"}},"b":{"namespace":"demo","labels":{"app.kubernetes.io/name":"web-degraded"}}}'; \
	echo "== story 3 (reachability): GET /readyz =="; \
	ready="$$(curl -s localhost:8080/readyz)"; echo "$$ready"; \
	echo "$$ready" | grep -q '"connected":true' || { echo "FAIL: /readyz not connected"; exit 1; }; \
	echo "== story 1 (deployment health): GET /deployments/unhealthy?namespace=demo =="; \
	body="$$(curl -s 'localhost:8080/deployments/unhealthy?namespace=demo')"; echo "$$body"; \
	echo "$$body" | grep -q '"name":"web-degraded"' || { echo "FAIL: web-degraded was not flagged"; exit 1; }; \
	echo "$$body" | grep -q '"name":"web-healthy"'  && { echo "FAIL: web-healthy was wrongly flagged"; exit 1; }; \
	echo "$$body" | grep -q '"unhealthyCount":1'    || { echo "FAIL: expected exactly 1 unhealthy in demo ns"; exit 1; }; \
	echo "== story 2 (network isolation): POST then DELETE /network-policies/isolate =="; \
	curl -s -X POST -H 'Content-Type: application/json' -d "$$iso" localhost:8080/network-policies/isolate | grep -q '"isolated":true' || { echo "FAIL: isolate did not report success"; exit 1; }; \
	n="$$(kubectl --kubeconfig "$(KIND_KUBECONFIG)" -n demo get netpol -l $$npsel --no-headers 2>/dev/null | wc -l | tr -d ' ')"; \
	[ "$$n" = "2" ] || { echo "FAIL: expected 2 isolation NetworkPolicies, got $$n"; exit 1; }; \
	curl -s -X DELETE -H 'Content-Type: application/json' -d "$$iso" localhost:8080/network-policies/isolate | grep -q '"isolated":false' || { echo "FAIL: de-isolate did not report success"; exit 1; }; \
	n2="$$(kubectl --kubeconfig "$(KIND_KUBECONFIG)" -n demo get netpol -l $$npsel --no-headers 2>/dev/null | wc -l | tr -d ' ')"; \
	[ "$$n2" = "0" ] || { echo "FAIL: isolation policies not cleaned up, $$n2 remain"; exit 1; }; \
	echo "PASS: readiness connected, web-degraded flagged (count=1), isolation created 2 + cleaned up"

kind-down: ## Delete the kind cluster + its kubeconfig
	-kind delete cluster --name $(KIND_CLUSTER)
	rm -f "$(KIND_KUBECONFIG)"
