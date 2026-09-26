# Kairn — commandes de développement (CLAUDE.md §8).
# Sous Windows sans make : scripts/make.ps1 <cible> couvre demo, test, lint et gen.

SHELL := /bin/bash
GO ?= go
COMPOSE ?= docker compose
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
REGISTRY ?= registry.kairn.io/kairn
GO_SERVICES := api:./services/api:kairn-api ingest:./services/ingest:kairn-ingest cost-engine:./services/cost-engine:kairn-cost-engine \
               notifier:./services/notifier:kairn-notifier cli:./cli:kairn agent:./agent:kairn-agent
PG_TEST_DSN ?= postgres://kairn_app:$(KAIRN_APP_PASSWORD)@127.0.0.1:5432/kairn_test?sslmode=disable
CH_TEST_URL ?= http://kairn:$(KAIRN_CLICKHOUSE_PASSWORD)@127.0.0.1:8123

-include .env
export

.PHONY: help env dev down logs seed demo migrate test test-go test-py test-tf test-web test-int e2e load lint fmt gen site docs build docker helm-test clean

help: ## Affiche cette aide
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

env: ## Génère .env avec des secrets aléatoires (une seule fois)
	@if [ -f .env ]; then echo ".env existe déjà"; else \
	  { echo "POSTGRES_PASSWORD=$$(openssl rand -hex 16)"; \
	    echo "KAIRN_OWNER_PASSWORD=$$(openssl rand -hex 16)"; \
	    echo "KAIRN_APP_PASSWORD=$$(openssl rand -hex 16)"; \
	    echo "KAIRN_CLICKHOUSE_PASSWORD=$$(openssl rand -hex 16)"; \
	    echo "KAIRN_S3_SECRET_KEY=$$(openssl rand -hex 16)"; \
	    echo "KAIRN_SESSION_SECRET=$$(openssl rand -hex 32)"; \
	    echo "KAIRN_SERVICE_TOKEN=$$(openssl rand -hex 32)"; \
	    echo "KAIRN_KEK=$$(openssl rand -base64 32)"; \
	    echo "KEYCLOAK_ADMIN_PASSWORD=$$(openssl rand -hex 12)"; \
	    echo "ANTHROPIC_API_KEY="; echo "MISTRAL_API_KEY="; } > .env; \
	  chmod 600 .env; echo ".env créé (ne jamais le versionner)"; fi

dev: env ## Pile locale complète (docker compose : pg, clickhouse, nats, valkey, keycloak, minio + services)
	$(COMPOSE) up -d --build
	@echo "Web : http://localhost:3000 · API : http://localhost:8080/api/v1/docs · make seed pour la démo"

down: ## Arrête la pile locale
	$(COMPOSE) down

logs: ## Journaux de la pile locale
	$(COMPOSE) logs -f --tail=100

migrate: ## Applique les migrations PostgreSQL et ClickHouse
	$(COMPOSE) run --rm migrate

seed: ## Charge l'organisation de démonstration (OpenStack + K8s) dans la pile locale
	$(COMPOSE) run --rm --entrypoint /usr/local/bin/app api seed

demo: ## Instance autonome en mémoire (sans Docker) : API :8080 + web :3000
	KAIRN_MODE=demo $(GO) run ./services/api & \
	(cd apps/web && npm run dev)

test: test-go test-py test-tf ## Tous les tests unitaires
test-go:
	$(GO) test ./... -count=1
test-tf:
	cd terraform-provider && $(GO) test ./... -count=1
test-py:
	cd services/analytics && python -m pytest -q
	cd services/ai && python -m pytest -q
test-web:
	cd apps/web && npm run typecheck && npm run lint

test-int: env ## Tests d'intégration (PostgreSQL avec RLS, ClickHouse) sur la pile docker compose
	$(COMPOSE) up -d postgres clickhouse
	@until $(COMPOSE) exec -T postgres pg_isready -U postgres -d kairn >/dev/null 2>&1; do sleep 1; done
	$(COMPOSE) exec -T postgres psql -U postgres -tAc "SELECT 1 FROM pg_database WHERE datname='kairn_test'" | grep -q 1 || \
	  $(COMPOSE) exec -T postgres psql -U postgres -c "CREATE DATABASE kairn_test OWNER kairn_owner"
	KAIRN_POSTGRES_MIGRATE_URL="postgres://kairn_owner:$(KAIRN_OWNER_PASSWORD)@127.0.0.1:5432/kairn_test?sslmode=disable" \
	  KAIRN_CLICKHOUSE_ADDR= $(GO) run ./services/api migrate
	KAIRN_TEST_PG_DSN="$(PG_TEST_DSN)" KAIRN_TEST_CLICKHOUSE_URL="http://127.0.0.1:8123" KAIRN_TEST_CLICKHOUSE_DB=kairn \
	  KAIRN_TEST_CLICKHOUSE_USER=kairn KAIRN_TEST_CLICKHOUSE_PASSWORD="$(KAIRN_CLICKHOUSE_PASSWORD)" \
	  $(GO) test -p 1 -count=1 -coverpkg=./... -coverprofile=coverage.out ./...
	scripts/coverage.sh coverage.out

e2e: ## Tests de bout en bout Playwright (instance de démo)
	cd test/e2e && npm ci && npx playwright install --with-deps chromium && npx playwright test

load: ## Scénarios de charge k6 (ingestion et tableaux de bord)
	k6 run test/load/dashboards.js
	k6 run test/load/ingestion.js

lint: ## golangci-lint, gofmt, ruff, mypy, eslint, tsc
	@test -z "$$(gofmt -l agent cli connectors migrations pkg services)" || { gofmt -l agent cli connectors migrations pkg services; exit 1; }
	$(GO) vet ./...
	@command -v golangci-lint >/dev/null && golangci-lint run ./... || echo "golangci-lint absent (installé en CI)"
	cd services/analytics && ruff check . && mypy kairn_analytics
	cd services/ai && ruff check . && mypy kairn_ai tests
	cd apps/web && npm run lint && npm run typecheck
	cd terraform-provider && test -z "$$(gofmt -l .)" && $(GO) vet ./...

fmt: ## Formate le code
	gofmt -w agent cli connectors migrations pkg services terraform-provider
	cd services/analytics && ruff format .
	cd services/ai && ruff format .

gen: ## Génère la spécification OpenAPI, la documentation des connecteurs et le client TypeScript
	$(GO) run ./services/api/cmd/openapi docs/api
	$(GO) run ./services/api/cmd/docs docs/connectors
	cd apps/web && npm run gen

site: ## Site vitrine statique (apps/site/out) ; NEXT_PUBLIC_SITE_URL, NEXT_PUBLIC_APP_URL, NEXT_PUBLIC_CONTACT_EMAIL
	cd apps/site && npm ci --no-audit --no-fund && npm run typecheck && npm run build

docs: ## Site de documentation (docs/*.md → apps/web/public/docs, servi sous /docs)
	cd apps/docs && npm ci --no-audit --no-fund && npm test && node build.mjs

build: ## Compile les binaires Go dans bin/
	@mkdir -p bin
	@for s in $(GO_SERVICES); do IFS=: read -r name pkg bin <<< "$$s"; \
	  echo "→ $$bin"; CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/$$bin $$pkg || exit 1; done

docker: ## Construit les images (VERSION, REGISTRY)
	@for s in $(GO_SERVICES); do IFS=: read -r name pkg bin <<< "$$s"; \
	  docker build -f deploy/docker/go.Dockerfile --build-arg PKG=$$pkg --build-arg BIN=$$bin --build-arg VERSION=$(VERSION) -t $(REGISTRY)/$$name:$(VERSION) . || exit 1; done
	docker build -f deploy/docker/python.Dockerfile --build-arg SERVICE=analytics --build-arg MODULE=kairn_analytics.app -t $(REGISTRY)/analytics:$(VERSION) .
	docker build -f deploy/docker/python.Dockerfile --build-arg SERVICE=ai --build-arg MODULE=kairn_ai.app -t $(REGISTRY)/ai:$(VERSION) .
	docker build -f apps/web/Dockerfile -t $(REGISTRY)/web:$(VERSION) .

helm-test: ## Lint et rendu du chart Helm
	helm lint deploy/helm/kairn --values deploy/helm/kairn/ci/test-values.yaml
	helm template kairn deploy/helm/kairn --values deploy/helm/kairn/ci/test-values.yaml > /dev/null
	helm template kairn deploy/helm/kairn --values deploy/helm/kairn/ci/single-node-values.yaml > /dev/null

clean: ## Supprime les artefacts locaux
	rm -rf bin dist coverage.* apps/web/.next
