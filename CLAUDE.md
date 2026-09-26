# CLAUDE.md — Kairn

> Nom de travail : **Kairn** (modifiable). Ce fichier est la référence produit ET technique du projet.
> Claude Code doit le lire avant toute tâche et le maintenir à jour quand une décision change.

---

## 1. Vision produit

**Kairn est une plateforme d'observabilité orientée coûts pour les clouds souverains, OpenStack et Kubernetes (cloud ou on-prem).**

Elle répond à trois questions, pour chaque service, projet, équipe ou client :
1. **Combien ça coûte ?** — coût réel, alloué et prévisionnel.
2. **Est-ce bien utilisé ?** — utilisation réelle croisée avec le coût, recommandations de dimensionnement.
3. **Pourquoi ça a bougé ?** — corrélation automatique entre variation de coût, métriques, déploiements et incidents, expliquée par l'IA.

### Positionnement
- Les outils FinOps existants (Vantage, CloudZero, Datadog CCM, Kubecost…) sont centrés AWS/Azure/GCP.
- Kairn cible ce qu'ils couvrent mal : **OVHcloud, Scaleway, Outscale, OpenStack privé, Kubernetes on-prem/hybride**, avec un argument **souveraineté** (hébergement UE, édition self-hosted, LLM européen possible).
- Kairn ne remplace pas une stack de monitoring : il **consomme l'existant** (Prometheus, Gnocchi, APIs cloud) et fournit un agent léger uniquement pour ceux qui n'ont rien.

### Personas
| Persona | Besoin principal | Surface principale |
|---|---|---|
| **Ingénieur DevOps/SRE** | Comprendre l'usage, dimensionner, diagnostiquer une dérive | Dashboards, explorer, assistant IA, CLI/MCP |
| **Responsable plateforme / CTO** | Allouer les coûts par équipe, piloter le budget | Allocation, budgets, showback |
| **DAF / dirigeant** | Savoir combien, pourquoi, et quoi faire | Rapport mensuel, vue exécutive |
| **ESN / infogéreur (MSP)** | Gérer plusieurs clients, refacturer | Multi-organisation, chargeback, rapports marque blanche |

---

## 2. Périmètre fonctionnel (produit complet)

Chaque module a un identifiant (`M-xx`) utilisé dans le code, les tickets et les commits.

### M-01 — Connecteurs & ingestion
- **Clouds** : OpenStack (Keystone, Nova, Cinder, Neutron, Swift, Octavia, Gnocchi/Ceilometer), OVHcloud (Public Cloud + facturation), Scaleway, Outscale. AWS/Azure/GCP en connecteur secondaire unique, via leurs exports de facturation au format FinOps **FOCUS** (ADR-0007).
- **Kubernetes** : API server (inventaire, requests/limits, HPA, events), Prometheus/VictoriaMetrics/Thanos (utilisation), OpenCost (import optionnel).
- **Déploiements & incidents** : webhooks GitLab/GitHub, Argo CD, Flux ; Alertmanager, PagerDuty, Opsgenie.
- **Agent Kairn** (Go, binaire unique) : collecte node/VM/conteneurs quand aucune stack métrique n'existe ; push OTLP.
- Ingestion **incrémentale et idempotente**, rejouable (backfill jusqu'à 13 mois).
- Chaque connecteur déclare : ressources couvertes, fréquence, permissions minimales requises (documentées), état de santé.

### M-02 — Inventaire & topologie
- Inventaire unifié des ressources (VM, volumes, IP, LB, buckets, nodes, pods, namespaces, workloads).
- Graphe de relations : projet → VM → volume ; cluster → node → pod → workload → équipe.
- Historisation (qui existait quand, avec quelle taille).
- Détection des ressources orphelines (volumes non attachés, IP non utilisées, snapshots anciens).

### M-03 — Modèle de coût
- **Grilles tarifaires publiques** importées et versionnées (OVH, Scaleway, Outscale…) : import quotidien depuis les API publiques des fournisseurs (`connectors/<fournisseur>/pricing.go`), version = empreinte du contenu, nouvelle version effective le lendemain ; les grilles d'exemple ne complètent jamais une grille officielle (ADR-0006).
- **Factures réelles** importées quand l'API le permet ; rapprochement estimé vs facturé, écart affiché.
- **Coût on-prem** : saisie du coût matériel (amortissement), électricité, licences, main-d'œuvre, puis ventilation par vCPU/Go RAM/Go stockage.
- Remises, engagements, crédits, devises (EUR par défaut), TVA optionnelle.
- **Coût Kubernetes** : répartition du coût des nodes par pod selon max(requests, usage) — méthode configurable ; coût idle et coûts partagés (control plane, monitoring, ingress) répartis selon règles.

### M-04 — Allocation, showback & chargeback
- Règles d'allocation par tags/labels, projets, namespaces, regex, ou mapping manuel.
- Hiérarchie : organisation → business unit → équipe → service → environnement.
- Coûts partagés : répartition proportionnelle, fixe ou pondérée.
- Showback (visibilité) et chargeback (export refacturation CSV / API).
- Taux de couverture d'allocation affiché (objectif : > 95 % des coûts attribués).

### M-05 — Monitoring & utilisation
- Métriques d'utilisation normalisées : CPU, RAM, disque, réseau, IOPS, requêtes/s.
- Vues **coût × utilisation** à chaque niveau (VM, workload, projet, équipe).
- **Métriques unitaires** : coût par requête, par utilisateur, par client, par transaction — l'utilisateur définit la métrique métier (source Prometheus ou API).
- **Uptime checks** HTTP/TCP/ICMP multi-régions + **status page** publique ou privée (liée aux services allouées).
- Rétention : brut 15 jours, agrégé 5 min 90 jours, agrégé 1 h 25 mois.

### M-06 — Recommandations
- Rightsizing VM et workloads K8s (requests/limits) sur fenêtre glissante (P95 configurable).
- Ressources orphelines/inactives, environnements hors prod allumés la nuit (proposition de planification d'arrêt).
- Changement de gamme/flavor, stockage mal tiéré, engagement/réservation rentable.
- Chaque recommandation contient : économie mensuelle estimée, niveau de risque, preuves (graphes), étapes d'application, et **commande/patch prêt à l'emploi** (CLI OpenStack, manifest K8s, Terraform).
- Cycle de vie : ouverte → acceptée / reportée / ignorée (avec motif) → appliquée → économie **mesurée** après application.

### M-07 — Anomalies & corrélation
- Détection d'anomalies de coût et d'usage (saisonnalité jour/semaine, baseline par série).
- **Corrélation automatique** : sur une anomalie, rechercher dans la fenêtre les déploiements, changements d'inventaire, événements HPA, incidents, pics de métriques.
- Explication générée par l'IA avec liens vers les preuves ; jamais d'affirmation sans donnée source.

### M-08 — Budgets & alerting
- Budgets par n'importe quel nœud de la hiérarchie, mensuels/trimestriels/annuels.
- Alertes : seuil réel, seuil prévisionnel (forecast de fin de période), anomalie, uptime, recommandation à fort impact.
- Canaux : e-mail, Slack, Teams, Mattermost, webhook, PagerDuty.
- Regroupement et déduplication, silences, heures ouvrées.

### M-09 — Prévisions
- Forecast de coût par nœud (modèle saisonnier + tendance), intervalle de confiance affiché.
- Simulation « what-if » : ajout de nodes, changement de flavor, migration d'un cloud à un autre.

### M-10 — IA : assistant & rapports
- **Assistant conversationnel** dans l'app : questions en langage naturel sur coûts, usage, anomalies ; répond avec graphes et liens vers les données.
- Architecture : l'IA n'accède aux données **que via des outils internes typés**, qui appellent l'API publique avec le jeton de l'utilisateur (donc exactement ses droits : RBAC, scopes, plan, isolation) — aucun SQL, même paramétré (ADR-0009) ; tous les chiffres viennent des outils, jamais du modèle.
- **Rapport mensuel exécutif** (PDF + e-mail) : dépense, évolution, top variations expliquées, économies réalisées, 3 actions prioritaires. Langage non technique.
- **Serveur MCP Kairn** exposé aux clients : leur agent IA (Claude, etc.) peut interroger Kairn avec les mêmes permissions que l'utilisateur.
- Fournisseur LLM abstrait : Anthropic (défaut SaaS), Mistral (option souveraine), modèle local via API compatible OpenAI (self-hosted).
- Suivi du coût LLM par organisation ; quotas par plan.

### M-11 — Multi-tenant, accès & MSP
- Organisations, espaces, équipes ; RBAC (Owner, Admin, Finance, Engineer, Viewer) + scopes par nœud d'allocation.
- SSO OIDC/SAML, SCIM, MFA.
- Mode **MSP** : une organisation parente gère des organisations clientes, rapports en marque blanche.
- Journal d'audit complet et exportable.

### M-12 — Intégrations & API
- API REST publique versionnée (`/api/v1`), OpenAPI 3.1 générée, tokens scoppés.
- Exports : CSV, Parquet, S3-compatible, webhooks.
- Provider Terraform (organisations, connecteurs, budgets, règles d'allocation).
- CLI `kairn` (Go).

### M-13 — Administration & facturation SaaS
- Onboarding guidé (connexion du premier cloud en < 10 min, premiers coûts visibles en < 1 h).
- Plans, essais, facturation (Stripe), limites par plan.
- Page santé des connecteurs, statut de la plateforme.

### Hors périmètre (explicite)
- APM / tracing applicatif, gestion de logs, SIEM.
- Exécution automatique des recommandations sans validation humaine (possible plus tard, derrière une option explicite).

---

## 3. Exigences non fonctionnelles

| Domaine | Exigence |
|---|---|
| **Souveraineté** | SaaS hébergé dans l'UE (OVH ou Scaleway). Aucune donnée client hors UE sauf appel LLM explicitement autorisé par l'organisation. Édition self-hosted complète (Helm). |
| **Sécurité** | Credentials clouds chiffrés (enveloppe, KMS / Vault). Permissions lecture seule par défaut. OWASP ASVS niveau 2. Scans SAST/DAST/dépendances en CI. Pentest avant GA. |
| **Conformité** | RGPD (DPA, registre, droit à l'effacement), trajectoire ISO 27001, compatibilité SecNumCloud visée pour l'édition self-hosted. |
| **Isolation** | Isolation stricte par `org_id` : Row-Level Security PostgreSQL + vérification applicative. Tests d'isolation obligatoires. |
| **Performance** | Dashboards < 1,5 s (P95) sur 12 mois de données pour une org de 10 000 ressources. Ingestion de 1 M points/min par worker. |
| **Disponibilité** | SLO 99,9 % (API + UI), 99,95 % alerting. |
| **Fiabilité des chiffres** | Tout montant est traçable jusqu'à sa source (ligne de facture, grille tarifaire versionnée, métrique). Écart estimé vs facturé < 2 % visé. |
| **Observabilité interne** | OpenTelemetry (traces, métriques, logs) sur tous les services. |
| **Accessibilité** | RGAA / WCAG 2.1 AA. |
| **i18n** | Français et anglais dès la v1 ; formats monétaires et dates localisés. |

---

## 4. Architecture

```
                    ┌──────────────┐     ┌──────────────┐
  Navigateur ──────▶│   web (Next) │────▶│  api (Go)    │◀──── CLI / Terraform / MCP
                    └──────────────┘     └──────┬───────┘
                                                │
             ┌──────────────────┬───────────────┼─────────────────┬────────────────┐
             ▼                  ▼               ▼                 ▼                ▼
      ┌────────────┐   ┌──────────────┐  ┌─────────────┐  ┌─────────────┐  ┌────────────┐
      │ PostgreSQL │   │  ClickHouse  │  │    NATS     │  │   Redis     │  │ Objet (S3) │
      │ (config,   │   │ (métriques,  │  │ (JetStream) │  │ (cache,     │  │ (exports,  │
      │  inventaire│   │  coûts,      │  └──────┬──────┘  │  rate-limit)│  │  rapports) │
      │  RBAC)     │   │  séries)     │         │         └─────────────┘  └────────────┘
      └────────────┘   └──────────────┘         │
                                                ▼
        ┌───────────────┬───────────────┬───────────────┬───────────────┬──────────────┐
        │ ingest-workers│ cost-engine   │ analytics     │ notifier      │ ai-service   │
        │ (connecteurs) │ (tarifs,      │ (anomalies,   │ (alertes,     │ (assistant,  │
        │               │  allocation)  │  reco, fcst)  │  canaux)      │  rapports,   │
        └───────────────┴───────────────┴───────────────┴───────────────┘  MCP)        │
                                                                          └──────────────┘
   Agent Kairn (Go) ── OTLP ──▶ ingest-gateway
   Uptime probes (multi-régions) ──▶ ingest-gateway
```

### Choix techniques
| Composant | Choix | Raison |
|---|---|---|
| Backend API & workers | **Go** (version fixée par `go.mod`), API **chi + huma v2** (OpenAPI 3.1 générée depuis les types) | Binaires légers, écosystème cloud-native, même langage que l'agent et la CLI |
| Analytics / ML | **Python 3.12** (service `analytics`) | Détection d'anomalies, forecast (statsmodels / Prophet-like) |
| IA | **Python 3.12** (service `ai-service`), abstraction fournisseurs | Écosystème LLM, SDK MCP |
| Frontend | **Next.js (App Router) + TypeScript + Tailwind + shadcn/ui**, graphes **ECharts** | Productivité, SSR pour la vue exécutive |
| Base relationnelle | **PostgreSQL 16** + RLS | Config, inventaire, RBAC, audit |
| Séries & coûts | **ClickHouse** | Agrégations massives rapides, rétention par TTL |
| Bus | **NATS JetStream** | Simple, léger, rejouable |
| Cache | **Redis / Valkey** | Limitation de débit partagée entre réplicas (ADR-0012) ; cache de requêtes et verrous à introduire si les tests de charge l'exigent |
| Auth | **Keycloak** (self-hosted) / OIDC externe | SSO, SAML, SCIM |
| Secrets | **Vault** ou KMS du cloud hôte | Chiffrement des credentials clients |
| Déploiement | **Kubernetes + Helm + Argo CD**, IaC **OpenTofu** | Même livrable SaaS et self-hosted |
| CI/CD | GitLab CI ou GitHub Actions | Lint, tests, build multi-arch, SBOM, signature cosign |

### Principes d'architecture
- Communication inter-services asynchrone par défaut (NATS) ; synchrone uniquement API → services pour la lecture.
- Chaque connecteur est un **plugin** implémentant l'interface `Connector` (voir §6) ; ajout d'un cloud = nouveau package, sans toucher au cœur.
- Le cost-engine est **déterministe et rejouable** : mêmes entrées + même version de grille = même résultat.
- Toute donnée client porte `org_id` ; aucune requête sans filtre `org_id` (en plus de la RLS — ADR-0002).
- **Mode mono-nœud** : sans NATS, l'API exécute aussi workers, ordonnanceur et passerelle d'ingestion (servie hors des middlewares de l'API) ; **mode démo** tout en mémoire (ADR-0008).
- `X-Forwarded-For` n'est accepté que des proxys de confiance (`KAIRN_TRUSTED_PROXIES`, ADR-0011).

---

## 5. Modèle de données (principal)

PostgreSQL :
- `organizations`, `users`, `memberships`, `roles`, `api_tokens`, `audit_events`
- `connectors` (type, config chiffrée, statut, last_sync)
- `resources` (id, org_id, connector_id, provider, type, external_id, name, region, attributes JSONB, labels JSONB, valid_from, valid_to)
- `resource_edges` (parent_id, child_id, relation)
- `price_catalogs`, `price_items` (provider, sku, unit, price, currency, valid_from)
- `onprem_cost_models`
- `allocation_nodes` (arbre), `allocation_rules`, `shared_cost_rules`
- `budgets`, `alert_rules`, `alert_events`, `notification_channels`
- `recommendations` (type, resource_id, savings_monthly, risk, status, evidence JSONB, remediation JSONB)
- `anomalies` (series_key, window, severity, correlated_events JSONB, explanation)
- `uptime_checks`, `status_pages`, `incidents`
- `reports`

ClickHouse :
- `metrics` (org_id, resource_id, metric, ts, value) — TTL par granularité
- `cost_lines` (org_id, resource_id, day, cost_type, amount, currency, source, catalog_version, allocation_node_id)
- `events` (org_id, ts, kind, source, payload) — déploiements, HPA, incidents
- Vues matérialisées d'agrégats horaires/journaliers.

Les migrations sont versionnées (`golang-migrate` pour PostgreSQL, fichiers SQL pour ClickHouse). Jamais de modification de schéma hors migration.

---

## 6. Contrats clés

### Interface connecteur (Go)
```go
type Connector interface {
    Type() string
    Validate(ctx context.Context, cfg Config) error          // vérifie accès et permissions
    RequiredPermissions() []Permission                        // documentées dans l'UI
    SyncInventory(ctx context.Context, since time.Time) (<-chan Resource, error)
    SyncMetrics(ctx context.Context, window TimeWindow) (<-chan MetricPoint, error)
    SyncBilling(ctx context.Context, period Period) (<-chan CostLine, error) // optionnel
    Health(ctx context.Context) HealthStatus
}
```

### Outils IA internes (ai-service)
`get_costs(filters, group_by, period)`, `get_usage(resource|node, metric, period)`, `list_recommendations(filters)`, `get_anomalies(period)`, `get_events(window)`, `get_budget_status(node)`, `render_chart(spec)`.
Liste complète et schémas : `services/ai/kairn_ai/tools.py` (mêmes outils pour l'assistant et le serveur MCP).
Règles : tous les montants cités proviennent d'un appel d'outil ; le prompt système interdit d'inventer un chiffre ; chaque réponse liste ses sources ; un contrôle d'ancrage signale tout nombre absent des résultats d'outils.

### API
- REST JSON, `/api/v1/...`, pagination par curseur, filtres cohérents, erreurs RFC 9457 (problem+json).
- OpenAPI générée depuis le code, publiée dans `docs/api/`.

---

## 7. Organisation du dépôt (monorepo)

```
/
├── CLAUDE.md
├── apps/
│   ├── web/                 # Next.js
│   └── docs/                # site de documentation (docs/*.md → HTML servi par web sous /docs)
├── services/
│   ├── api/                 # Go — API publique + BFF
│   ├── ingest/              # Go — workers d'ingestion + gateway OTLP
│   ├── cost-engine/         # Go — tarification, allocation
│   ├── notifier/            # Go — alertes et canaux
│   ├── analytics/           # Python — anomalies, reco, forecast
│   └── ai/                  # Python — assistant, rapports, serveur MCP
├── connectors/              # Go — un package par fournisseur
│   ├── openstack/  ovh/  scaleway/  outscale/  kubernetes/  prometheus/ ...
├── agent/                   # Go — agent Kairn
├── cli/                     # Go — CLI kairn
├── terraform-provider/      # module Go distinct (dépendances HashiCorp isolées)
├── pkg/                     # Go — code partagé (modèles, auth, tenancy, otel)
├── migrations/{postgres,clickhouse}/
├── deploy/
│   ├── helm/kairn/
│   ├── argocd/
│   └── tofu/                # infra SaaS
├── docs/
│   ├── adr/                 # Architecture Decision Records
│   ├── api/
│   └── connectors/          # permissions requises par connecteur (générées : make gen)
└── test/
    ├── e2e/                 # Playwright
    ├── fixtures/            # jeux de données de coûts/métriques réalistes
    └── load/                # k6
```

---

## 8. Commandes

```bash
make dev            # stack locale complète (docker compose : pg, clickhouse, nats, redis, keycloak)
make seed           # charge des fixtures réalistes (org de démo OpenStack + K8s)
make test           # tous les tests unitaires
make test-int       # tests d'intégration (testcontainers)
make e2e            # Playwright
make lint           # golangci-lint, ruff, eslint, tsc
make migrate        # applique les migrations
make gen            # génère OpenAPI, documentation des connecteurs, client TypeScript
make docs           # site de documentation (apps/docs → apps/web/public/docs)
make load           # scénarios de charge k6 (seuils du §3)
make helm-test      # lint + template du chart
```
Si une commande n'existe pas encore, la créer dans le Makefile plutôt que de lancer des commandes ad hoc. Sous Windows sans make : `scripts/make.ps1 <cible>`.
Import manuel des grilles publiques : `kairn-api import-prices [--providers ovh,scaleway,outscale]`.

---

## 9. Conventions de code

- **Go** : standard `gofmt`/`golangci-lint` ; erreurs enveloppées avec contexte (`fmt.Errorf("...: %w", err)`) ; `context.Context` en premier paramètre ; pas de variables globales mutables ; logs structurés (`slog`) sans données sensibles.
- **Python** : `ruff` + `mypy --strict`, Pydantic pour les schémas, `uv` pour les dépendances.
- **TypeScript** : `strict: true`, pas de `any`, composants serveur par défaut, données via client API généré.
- **Montants** : jamais de float pour l'argent. Go : `decimal`, Python : `Decimal`, ClickHouse : `Decimal(18,6)`, JSON : chaîne. Toujours accompagnés de la devise.
- **Temps** : UTC en base, conversion au rendu ; granularité et fuseau explicites dans toutes les requêtes.
- **Commits** : Conventional Commits avec module, ex. `feat(M-06): rightsizing des requests K8s`.
- **ADR** : toute décision structurante (nouvelle dépendance, changement de modèle de coût, choix d'algorithme) donne lieu à un ADR dans `docs/adr/`.

---

## 10. Tests & qualité

- Couverture minimale : 80 % sur `cost-engine`, `connectors`, `pkg/tenancy` ; 70 % ailleurs.
- **Tests « golden »** sur le cost-engine : fixtures d'entrée → montants attendus au centime. Toute modification de résultat doit être justifiée dans la PR.
- **Tests d'isolation multi-tenant** automatiques : chaque endpoint est testé avec un token d'une autre organisation.
- Connecteurs : tests contre des réponses API enregistrées (fixtures) + tests d'intégration optionnels contre un DevStack.
- IA : jeu d'évaluation (questions → réponse attendue / chiffres attendus) exécuté en CI ; échec si un chiffre ne correspond pas aux outils.
- Charge : scénarios k6 sur ingestion et dashboards, seuils des §3 vérifiés avant chaque release.

---

## 11. Règles pour Claude Code

1. Lire ce fichier et le README du module concerné avant de coder.
2. Ne jamais écrire de requête sans filtre `org_id` ; ne jamais désactiver la RLS.
3. Ne jamais journaliser de credentials, tokens ou données de facture brutes.
4. Ne jamais utiliser de float pour un montant.
5. Toute nouvelle fonctionnalité : tests + mise à jour de la doc + entrée de changelog.
6. Une tâche = une PR ciblée ; pas de refactor opportuniste hors périmètre.
7. En cas d'ambiguïté fonctionnelle, proposer les options dans la PR plutôt que trancher silencieusement ; si la décision est structurante, rédiger un ADR.
8. Les connecteurs sont en **lecture seule** ; aucune action d'écriture sur l'infra d'un client sans fonctionnalité explicitement spécifiée.
9. Mettre à jour ce fichier quand une décision le rend inexact.

---

## 12. Modèle commercial (référence)

| Plan | Cible | Inclus | Prix indicatif |
|---|---|---|---|
| **Starter** | PME, 1 cloud | Coûts, usage, reco, budgets, 3 utilisateurs | ~99 €/mois |
| **Team** | Scale-ups, multi-cloud + K8s | + allocation, anomalies, assistant IA, rapports, SSO | ~1 % de la dépense suivie, min. 490 €/mois |
| **Enterprise** | Grands comptes, secteur public | + self-hosted, SCIM, LLM souverain, SLA, support dédié | Sur devis |
| **MSP** | Infogéreurs | Multi-clients, marque blanche, chargeback | Par client géré |

Les limites de plan sont appliquées côté API (M-13), jamais uniquement côté UI.

---

## 13. Feuille de route des releases

Le produit complet est livré en releases successives ; chaque release est vendable.

| Release | Contenu | Critère de sortie |
|---|---|---|
| **R1 — Fondations** | M-01 (OpenStack, K8s, Prometheus), M-02, M-03, M-11 (base), UI coûts × usage | Coûts d'une org OpenStack + K8s calculés à ±2 % de la facture |
| **R2 — Agir** | M-04, M-06, M-08, connecteurs OVH + Scaleway | 3 clients pilotes, économies mesurées |
| **R3 — Comprendre** | M-07, M-09, M-10 (assistant + rapports), agent Kairn | Rapport mensuel envoyé automatiquement aux pilotes |
| **R4 — Écosystème** | M-05 uptime + status page, M-12 (API publique, Terraform, CLI, MCP), Outscale | API publique stable v1 |
| **R5 — Entreprise** | Self-hosted Helm, SAML/SCIM, MSP, audit, LLM souverain, M-13 complet | Pentest passé, GA commerciale |

---

## 14. Glossaire

- **Showback** : afficher à une équipe ce qu'elle coûte. **Chargeback** : lui refacturer.
- **Rightsizing** : ajuster la taille d'une ressource à son usage réel.
- **Coût idle** : capacité payée non réservée ni utilisée (ex. nodes K8s sous-remplis).
- **Coût unitaire** : coût rapporté à une métrique métier (par requête, par client…).
- **Coût partagé** : coût commun (control plane, monitoring) réparti selon une règle.
