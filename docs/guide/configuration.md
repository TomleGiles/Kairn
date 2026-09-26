# Configuration

Tous les services se configurent par variables d'environnement. En production (`KAIRN_MODE=production`, défaut), les variables obligatoires manquantes empêchent le démarrage avec la liste de ce qui manque. Les secrets proviennent de secrets Kubernetes (Helm) ou de `.env` (docker compose, généré par `make env`), jamais du dépôt.

## Services Go (API, ingestion, cost-engine, notifier)

| Variable | Défaut | Rôle |
|---|---|---|
| `KAIRN_MODE` | `production` | `demo` : tout en mémoire, connexion de développement ([ADR-0008](../adr/0008-mode-mono-noeud-et-demo.md)) |
| `KAIRN_HTTP_ADDR` | `:8080` | Adresse d'écoute |
| `KAIRN_PUBLIC_URL` | `http://localhost:3000` | URL publique de l'interface (liens des e-mails, rapports) |
| `KAIRN_API_URL` | `http://localhost:8080` | URL de l'API vue des autres services |
| `KAIRN_LOG_LEVEL` / `KAIRN_LOG_FORMAT` | `info` / `json` | Journalisation structurée (jamais de secret ni de donnée de facture brute) |
| `KAIRN_POSTGRES_URL` | — (**requis**) | DSN du rôle applicatif `kairn_app` (NOBYPASSRLS) |
| `KAIRN_POSTGRES_MIGRATE_URL` | `KAIRN_POSTGRES_URL` | DSN du rôle propriétaire pour `kairn-api migrate` |
| `KAIRN_CLICKHOUSE_ADDR` | — (**requis**) | URL HTTP de ClickHouse |
| `KAIRN_CLICKHOUSE_DB` / `_USER` / `_PASSWORD` | `kairn` / `default` / — | Accès ClickHouse |
| `KAIRN_NATS_URL` | — | NATS JetStream ; absent pour l'API → mode mono-nœud ; **requis** pour ingest, cost-engine, notifier |
| `KAIRN_REDIS_URL` | — | Valkey/Redis (`redis://`, `rediss://`) : limitation de débit partagée |
| `KAIRN_S3_ENDPOINT` / `_BUCKET` / `_ACCESS_KEY` / `_SECRET_KEY` / `_REGION` / `_SSL` | — / `kairn` / — / — / `gra` / `true` | Stockage objet des rapports et exports ; absent → disque local (`KAIRN_LOCAL_OBJECT_DIR`, défaut `.data/objects`) |
| `KAIRN_SESSION_SECRET` | — (**requis** pour l'API, ≥ 32 caractères) | Signature des sessions |
| `KAIRN_SESSION_TTL` | `12h` | Durée des sessions |
| `KAIRN_KEK` | — | Clé maître (base64, 32 octets) du chiffrement des credentials ; **requis** sans Vault |
| `KAIRN_VAULT_ADDR` / `_TOKEN` / `_TRANSIT_KEY` | — / — / `kairn` | Vault Transit à la place de `KAIRN_KEK` |
| `KAIRN_SERVICE_TOKEN` | — (**requis**) | Jeton partagé entre services internes |
| `KAIRN_OIDC_ISSUER` / `_CLIENT_ID` / `_CLIENT_SECRET` | — / `kairn` / — | SSO OIDC (Keycloak ou externe) ; SAML, MFA et fédération via Keycloak |
| `KAIRN_DEV_LOGIN` | `false` | Connexion sans mot de passe (développement uniquement ; forcé en démo) |
| `KAIRN_AI_URL` / `KAIRN_ANALYTICS_URL` | — | URL des services Python |
| `KAIRN_STRIPE_SECRET_KEY` / `_WEBHOOK_SECRET` / `KAIRN_STRIPE_PRICES` | — | Facturation SaaS (M-13) ; `KAIRN_STRIPE_PRICES` = `plan=price_id,…` |
| `KAIRN_SMTP_ADDR` / `_FROM` / `_USER` / `_PASSWORD` | — / `Kairn <no-reply@kairn.local>` | E-mails (alertes, rapports) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` / `OTEL_SERVICE_NAME` | — / `kairn-<service>` | OpenTelemetry (traces, métriques) |
| `KAIRN_CORS_ORIGINS` | — | Origines autorisées (l'interface est servie sous la même origine : inutile par défaut) |
| `KAIRN_TRUSTED_PROXIES` | — | Proxys dont `X-Forwarded-For` est accepté ([ADR-0011](../adr/0011-ip-cliente-et-proxys-de-confiance.md)) |
| `KAIRN_PRICE_IMPORT` | `ovh,scaleway,outscale` (démo : aucun) | Grilles publiques importées chaque jour ; `none` pour désactiver ([ADR-0006](../adr/0006-grilles-tarifaires-publiques.md)) |
| `KAIRN_DEMO_SEED` | `true` | Organisation d'exemple au démarrage du mode démo |
| `KAIRN_REGION` | `eu-west-gra` | Région d'une sonde d'uptime (`kairn-ingest probe`) |
| `KAIRN_GATEWAY_URL` | — | Passerelle contactée par une sonde |
| `KAIRN_PROBE_ALLOW_PRIVATE` | `false` | Autorise les sondes vers des adresses privées (self-hosted ; protection SSRF sinon) |

## Services Python

| Variable | Défaut | Rôle |
|---|---|---|
| `KAIRN_API_URL`, `KAIRN_SERVICE_TOKEN` | — | Accès à l'API Kairn |
| `ANTHROPIC_API_KEY` | — | Fournisseur Anthropic (défaut SaaS) |
| `KAIRN_ANTHROPIC_MODEL` / `KAIRN_ANTHROPIC_EFFORT` | `claude-opus-5` / `medium` | Modèle et niveau d'effort |
| `MISTRAL_API_KEY`, `KAIRN_MISTRAL_URL`, `KAIRN_MISTRAL_MODEL` | — / `https://api.mistral.ai/v1` / `mistral-large-latest` | Option souveraine |
| `KAIRN_MISTRAL_PRICE_IN` / `_OUT` | `2` / `6` | Prix (EUR / million de tokens) pour la comptabilisation |
| `KAIRN_LOCAL_LLM_URL` / `_KEY` / `_MODEL` | `http://localhost:8000/v1` / — / `mistral-small` | Modèle local compatible OpenAI (self-hosted) |
| `KAIRN_ASSISTANT_MAX_TURNS` | `8` | Nombre maximal de tours d'outils par question |
| `KAIRN_REPORT_FONT` | — | Police TTF des rapports PDF |
| `KAIRN_PUBLIC_URL` | `http://localhost:3000` | Liens des rapports |
| `KAIRN_HOST` / `KAIRN_PORT` | `0.0.0.0` / `8090` (analytics) | Écoute |

## Interface web

| Variable | Défaut | Rôle |
|---|---|---|
| `KAIRN_API_URL` | `http://localhost:8080` | API (réécriture `/api`, `/ingest`, `/scim` sous la même origine) |
| `KAIRN_OIDC_ENABLED` | `false` | Affiche la connexion SSO |
| `KAIRN_DEV_LOGIN` | `true` | Affiche la connexion de développement |

## Agent et CLI

- Agent : `KAIRN_AGENT_GATEWAY`, `KAIRN_AGENT_TOKEN` ou `KAIRN_AGENT_TOKEN_FILE`, `KAIRN_AGENT_HOST_ROOT` (voir [agent](../connectors/agent.md)).
- CLI : `KAIRN_URL`, `KAIRN_TOKEN`, `KAIRN_ORG`, `KAIRN_CONFIG` (voir [API, CLI et MCP](api.md)).
