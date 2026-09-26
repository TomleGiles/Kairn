# Kairn

**Observabilité orientée coûts pour les clouds souverains, OpenStack et Kubernetes.**

Kairn répond à trois questions, pour chaque service, projet, équipe ou client :

1. **Combien ça coûte ?** — coût réel, alloué et prévisionnel ;
2. **Est-ce bien utilisé ?** — utilisation réelle croisée avec le coût, recommandations de dimensionnement ;
3. **Pourquoi ça a bougé ?** — corrélation automatique entre variation de coût, métriques, déploiements et incidents, expliquée par l'IA.

Kairn cible ce que les outils FinOps centrés AWS/Azure/GCP couvrent mal — **OVHcloud, Scaleway, OUTSCALE, OpenStack privé, Kubernetes on-prem ou hybride** — avec un argument de souveraineté : hébergement UE, édition self-hosted complète, LLM européen ou local. Kairn consomme la pile de monitoring existante (Prometheus, Gnocchi, API des clouds) et fournit un agent léger uniquement pour ceux qui n'ont rien.

## Essayer

```bash
make demo          # tout en mémoire : API :8080, interface :3000 (connexion demo@kairn.local)
```

Pile complète (PostgreSQL, ClickHouse, NATS, Valkey, MinIO, Keycloak) : `make env && make dev && make seed`. Voir le [guide de démarrage](docs/guide/demarrage.md).

## Fonctionnalités

| Module | Contenu |
|---|---|
| M-01 Connecteurs | OpenStack (Nova, Cinder, Neutron, Octavia, Swift, Gnocchi), OVHcloud, Scaleway, OUTSCALE, Kubernetes, Prometheus/VictoriaMetrics/Thanos, exports FOCUS (AWS, Azure, GCP), webhooks GitLab/GitHub/Argo CD/Flux/Alertmanager/PagerDuty/Opsgenie, agent Kairn (OTLP) — tous en lecture seule, backfill jusqu'à 13 mois |
| M-02 Inventaire | Inventaire unifié historisé, topologie (projet → VM → volume, cluster → node → pod → workload), ressources orphelines |
| M-03 Coûts | Grilles publiques importées chaque jour et versionnées, grilles négociées, factures réelles et rapprochement, coûts on-prem, répartition Kubernetes max(requests, usage) et coût idle, remises, engagements, crédits, TVA |
| M-04 Allocation | Hiérarchie organisation → business unit → équipe → service → environnement, règles par labels/projets/namespaces/regex, coûts partagés, showback et chargeback |
| M-05 Usage | Coût × utilisation à tous les niveaux, métriques unitaires (coût par requête, par client…), sondes d'uptime multi-régions et pages de statut |
| M-06 Recommandations | Rightsizing VM et workloads, orphelins, arrêts hors heures ouvrées, changement de gamme, stockage, engagements — avec économie estimée, risque, preuves, commande prête à l'emploi et économie mesurée après application |
| M-07 Anomalies | Détection saisonnière, corrélation avec déploiements, inventaire, HPA et incidents, explication générée à partir des preuves |
| M-08 Budgets | Budgets à tous les niveaux, alertes sur réel et prévision, e-mail, Slack, Teams, Mattermost, webhook, PagerDuty, silences et déduplication |
| M-09 Prévisions | Prévision saisonnière avec intervalle de confiance, simulations what-if |
| M-10 IA | Assistant conversationnel ancré sur les données, rapport mensuel exécutif (PDF + e-mail), serveur MCP ; Anthropic, Mistral ou modèle local |
| M-11 Accès | Multi-tenant (RLS PostgreSQL), RBAC, scopes d'allocation, SSO OIDC/SAML, SCIM, MFA, mode MSP, journal d'audit |
| M-12 Intégrations | API REST `/api/v1` (OpenAPI 3.1), exports CSV/Parquet/S3, webhooks, CLI `kairn`, provider Terraform/OpenTofu |
| M-13 SaaS | Onboarding guidé, plans et limites appliqués côté API, facturation Stripe, santé des connecteurs |

## Architecture

API et workers en **Go**, analytique et IA en **Python**, interface **Next.js**. PostgreSQL (configuration, inventaire, RBAC — Row-Level Security), ClickHouse (métriques, coûts, événements), NATS JetStream, Valkey, stockage S3. Déploiement Kubernetes (Helm, Argo CD, OpenTofu) ; mode mono-nœud pour les petites installations. Détails : [CLAUDE.md](CLAUDE.md) (référence produit et technique) et [décisions d'architecture](docs/adr/README.md).

```
apps/web            interface Next.js         services/api        API publique (Go)
apps/docs           site de documentation     services/ingest     workers, passerelle, ordonnanceur, sondes
connectors/         un package par source     services/cost-engine, notifier
pkg/                code Go partagé           services/analytics  anomalies, prévisions, recommandations (Python)
agent/, cli/        agent et CLI kairn        services/ai         assistant, rapports, serveur MCP (Python)
terraform-provider/ provider Terraform        deploy/             Helm, Argo CD, OpenTofu, docker
migrations/         PostgreSQL, ClickHouse    test/               e2e Playwright, charge k6
```

## Développement

| Commande | Rôle |
|---|---|
| `make test` | Tests unitaires Go, Python et provider Terraform |
| `make test-int` | Intégration PostgreSQL (RLS, rôle applicatif) et ClickHouse |
| `make e2e` | Parcours Playwright et accessibilité (WCAG 2.1 AA) sur l'instance de démo |
| `make load` | Scénarios k6 (tableaux de bord < 1,5 s P95, 1 M points/min) |
| `make lint` | golangci-lint, gofmt, ruff, mypy, eslint, tsc |
| `make gen` | OpenAPI, documentation des connecteurs, client TypeScript |
| `make docs` | Site de documentation (servi par l'interface sous `/docs`) |
| `make site` | Site vitrine statique (`apps/site/out`, à publier sur votre domaine) |

Règles du projet (isolation par `org_id`, jamais de flottant pour l'argent, connecteurs en lecture seule, ADR pour toute décision structurante…) : voir [CLAUDE.md](CLAUDE.md) §9–§11.

## Documentation

- [Documentation](docs/README.md) : démarrage, déploiement, configuration, modèle de coût, sécurité, API/CLI/MCP.
- [Connecteurs](docs/connectors/README.md) : permissions minimales et configuration de chaque source.
- [CHANGELOG](CHANGELOG.md).
