# Déploiement

Le même livrable sert le SaaS et l'édition self-hosted : images OCI multi-architectures signées (cosign, SBOM) et chart Helm `deploy/helm/kairn`.

## Composants

| Composant | Rôle | Réplicas |
|---|---|---|
| `kairn-api` | API REST publique + BFF (`serve`), migrations (`migrate`), import des grilles (`import-prices`) | ≥ 2 |
| `kairn-ingest gateway` | Webhooks entrants, OTLP de l'agent, résultats des sondes | ≥ 2 |
| `kairn-ingest worker` | Synchronisation des connecteurs | selon le nombre de connecteurs |
| `kairn-ingest scheduler` | Tâches périodiques (synchronisations, calculs, rapports, import des grilles) | **1** |
| `kairn-ingest probe` | Sondes d'uptime d'une région (`KAIRN_REGION`), sans accès aux bases | 1 par région |
| `kairn-cost-engine` | Tarification, allocation, rapprochement | ≥ 1 |
| `kairn-notifier` | Alertes et canaux | ≥ 1 |
| `analytics` (Python) | Anomalies, prévisions, recommandations | ≥ 1 |
| `ai` (Python) | Assistant, rapports, serveur MCP | ≥ 1 |
| `web` (Next.js) | Interface | ≥ 2 |

Dépendances : PostgreSQL 16, ClickHouse, NATS JetStream, Valkey/Redis, stockage objet S3-compatible, Keycloak ou autre fournisseur OIDC, Vault ou KMS (facultatif), SMTP.

## PostgreSQL : deux rôles

L'isolation multi-tenant repose sur la Row-Level Security ([ADR-0002](../adr/0002-isolation-multi-tenant-rls.md)). Créer les rôles une fois avec [`deploy/postgres/init-roles.sql`](../../deploy/postgres/init-roles.sql) :

- `kairn_owner` (propriétaire, migrations) → secret `postgres-migrate-url` ;
- `kairn_app` (**NOBYPASSRLS**, services) → secret `postgres-url`.

Ne jamais connecter les services avec un superutilisateur ou un rôle `BYPASSRLS`.

## Helm

```bash
kubectl create namespace kairn
kubectl -n kairn create secret generic kairn-secrets \
  --from-literal=postgres-url='postgres://kairn_app:…@pg:5432/kairn?sslmode=require' \
  --from-literal=postgres-migrate-url='postgres://kairn_owner:…@pg:5432/kairn?sslmode=require' \
  --from-literal=clickhouse-password='…' --from-literal=session-secret="$(openssl rand -hex 32)" \
  --from-literal=service-token="$(openssl rand -hex 32)" --from-literal=kek="$(openssl rand -base64 32)"
helm upgrade --install kairn deploy/helm/kairn -n kairn -f my-values.yaml
```

Le secret (`secrets.existingSecret`, défaut `kairn-secrets`) porte aussi, selon les options : `vault-token`, `s3-secret-key`, `oidc-client-secret`, `smtp-password`, `anthropic-api-key`, `mistral-api-key`, `stripe-secret-key`, `stripe-webhook-secret`. Aucune valeur sensible dans `values.yaml`.

Principales valeurs (`values.yaml`) :

- `mode` : `distributed` (un Deployment par service, NATS obligatoire) ou `single-node` (l'API exécute workers, ordonnanceur et passerelle, sans NATS — [ADR-0008](../adr/0008-mode-mono-noeud-et-demo.md), exemple `ci/single-node-values.yaml`) ;
- `publicURL`, `apiURL`, `ingress.*` ;
- `config.trustedProxies` (réseaux des ingress, jamais `0.0.0.0/0` — [ADR-0011](../adr/0011-ip-cliente-et-proxys-de-confiance.md)) ;
- `config.priceImport` : fournisseurs dont la grille publique est importée chaque jour (liste vide sans accès sortant — [ADR-0006](../adr/0006-grilles-tarifaires-publiques.md)) ;
- `redis.url` : limitation de débit partagée entre réplicas ([ADR-0012](../adr/0012-limitation-de-debit-partagee.md)) ;
- `nats.url`, `clickhouse.*`, `postgres.sslmode`, `s3.*` (vide = disque local, mono-nœud uniquement), `vault.*` ;
- `ai.*` : modèles Anthropic et Mistral, modèle local (`ai.localLLM`) ; les appels hors UE exigent l'accord de chaque organisation.

Le job de migration (hook Helm `pre-install` / `pre-upgrade`) s'exécute avec le rôle propriétaire avant le déploiement des services. `make helm-test` valide le chart (lint + rendu des jeux de valeurs de `ci/`).

## Argo CD et OpenTofu

- `deploy/argocd/` : `AppProject` et applications `staging` / `production` (valeurs par environnement dans `deploy/argocd/values/`).
- `deploy/tofu/` : infrastructure SaaS sur OVHcloud Public Cloud (réseau privé, Managed Kubernetes et ses node pools, PostgreSQL managé, utilisateur et stockage objet S3) ; un fichier de variables par environnement dans `environments/`.

## Souveraineté

- SaaS hébergé dans l'UE (OVHcloud ou Scaleway) ; aucune donnée client hors UE, sauf appel LLM explicitement autorisé par l'organisation.
- Édition self-hosted complète : LLM souverain (Mistral) ou local (API compatible OpenAI), import des grilles désactivable (`priceImport: []`), aucun appel sortant obligatoire.

## Sauvegardes

- PostgreSQL : sauvegarde continue (WAL) — configuration, inventaire, RBAC, audit.
- ClickHouse : sauvegardes des tables `cost_lines`, `billing_lines`, `metrics_1h` au minimum ; les coûts peuvent être recalculés depuis l'inventaire et les grilles versionnées (`POST /costs/recompute`).
- Stockage objet : rapports et exports.
