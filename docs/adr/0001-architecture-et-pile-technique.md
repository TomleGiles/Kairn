# 0001 — Architecture et pile technique

- Statut : acceptée
- Date : 2026-09-26
- Modules : tous

## Contexte

CLAUDE.md §4 fixe l'architecture cible (API Go, workers Go, services Python pour l'analytique et l'IA, PostgreSQL, ClickHouse, NATS JetStream, Redis/Valkey, stockage objet) et un livrable identique en SaaS et en self-hosted. Cet ADR consigne les bibliothèques retenues pour la mettre en œuvre et les écarts assumés.

## Décision

**Go (module unique `github.com/kairn-io/kairn`)** pour l'API, les workers (`services/ingest`, `services/cost-engine`, `services/notifier`), les connecteurs, l'agent et la CLI :

| Besoin | Bibliothèque | Raison |
|---|---|---|
| Routage HTTP | `go-chi/chi` | Middleware standard `net/http`, sans magie |
| API REST + OpenAPI 3.1 | `danielgtaylor/huma` v2 | OpenAPI générée depuis les types Go, erreurs RFC 9457 |
| PostgreSQL | `jackc/pgx` v5 | Pilote natif, transactions explicites (RLS, ADR-0002) |
| Migrations | `golang-migrate` (PostgreSQL) ; fichiers SQL embarqués appliqués par `pkg/migrate` (ClickHouse) | Versionnement strict (CLAUDE.md §5) |
| ClickHouse | client HTTP interne (`pkg/tsdb/chtsdb`) | Requêtes paramétrées typées (`{nom:Type}`), aucune dépendance lourde |
| Bus | `nats-io/nats.go` (JetStream) | Rejouable, groupes de consommateurs |
| Limitation de débit | `redis/go-redis` v9 (ADR-0012) | État partagé entre réplicas |
| Montants | `shopspring/decimal` (ADR-0004) | Jamais de float |
| Stockage objet | `minio-go` v7 | S3-compatible (OVHcloud, Scaleway, MinIO) |
| Export Parquet | `parquet-go` | Exports M-12 |
| Auth | `coreos/go-oidc`, `golang-jwt` | OIDC (Keycloak ou externe), jetons de session |
| Observabilité | OpenTelemetry (traces, métriques OTLP) | Exigence §3 |

**Python 3.12** (`uv`, `ruff`, `mypy --strict`, Pydantic) pour `services/analytics` (anomalies, prévisions, recommandations) et `services/ai` (assistant, rapports, serveur MCP, SDK `anthropic` et `mcp`).

**Next.js (App Router) + TypeScript strict + Tailwind + ECharts** pour `apps/web` ; client typé généré depuis l'OpenAPI (`openapi-typescript`). L'API est servie sous la même origine (réécriture `/api`), sans CORS.

**Contrats** : les connecteurs implémentent `connector.Connector` et s'enregistrent (`connector.Register`) ; ajouter un fournisseur ne touche pas au cœur. Les services communiquent par NATS (sujets `pkg/bus`), sauf les lectures synchrones API → services Python.

## Conséquences

- Un seul module Go simplifie le partage des modèles (`pkg/model`) ; `apps/` porte un `go.mod` factice pour exclure `node_modules` de `./...`.
- La documentation API (`docs/api/openapi.*`) et celle des connecteurs (`docs/connectors/`) sont **générées** (`make gen`) ; des tests échouent si elles divergent du code.
- Redis/Valkey n'est utilisé que pour la limitation de débit ; aucun cache de requêtes n'est nécessaire à ce stade (ClickHouse tient les objectifs de latence), à réévaluer avec les tests de charge.

## Alternatives écartées

- *gRPC entre services* : la frontière utile est asynchrone (NATS) ; REST suffit pour les lectures.
- *Pilote ClickHouse natif* : protocole binaire plus rapide mais dépendance lourde ; l'interface HTTP suffit aux volumes visés et reste compatible avec les offres managées.
