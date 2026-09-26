# API, CLI, Terraform et MCP

Toutes les intégrations passent par la même **API publique** et les mêmes contrôles d'accès que l'interface : RBAC, scopes d'allocation, limites de plan et isolation de l'organisation.

## Jetons d'API

*Paramètres → Jetons d'API* (ou `POST /api/v1/orgs/{org}/tokens`) : un jeton `kairn_…` est lié à **une** organisation, à un rôle (Admin, Finance, Engineer, Viewer) et, optionnellement, à une liste de permissions (`costs:read`, `export`…) et une date d'expiration. Il n'est affiché qu'à sa création ; Kairn n'en conserve qu'une empreinte.

## API REST

- Base : `https://<kairn>/api/v1`, JSON, authentification `Authorization: Bearer kairn_…`.
- Spécification **OpenAPI 3.1** générée depuis le code : [`docs/api/openapi.yaml`](../api/openapi.yaml) ; documentation interactive sur `/api/v1/docs`.
- Erreurs **RFC 9457** (`application/problem+json`) : `status`, `title`, `detail` et, pour une validation, la liste `errors` (emplacement et message). Les champs inconnus d'un corps de requête sont refusés.
- Montants : **chaînes décimales** (`"1234.500000"`), toujours accompagnés de leur devise ; dates en UTC, RFC 3339.
- Listes paginées par curseur (`items`, `next_cursor`) ; limitation de débit par jeton (réponse `429` avec `Retry-After`).

```bash
curl -H "Authorization: Bearer $KAIRN_TOKEN" \
  "https://kairn.example.com/api/v1/orgs/$ORG/costs?from=2026-09-01T00:00:00Z&to=2026-10-01T00:00:00Z&granularity=month&group_by=provider"
```

Principales ressources : `costs` (agrégats, lignes, export CSV/Parquet, recalcul), `usage`, `efficiency`, `resources`, `topology`, `allocation/{nodes,rules,shared-rules,coverage}`, `chargeback`, `recommendations`, `anomalies`, `budgets`, `alert-rules`, `channels`, `forecast`, `whatif`, `reports`, `connectors`, `pricing/{catalogs,adjustments,onprem-models}`, `exports`, `webhooks`, `uptime`, `status-pages`, `audit`.

## Webhooks sortants et exports

- **Webhooks** : `alert.fired`, `anomaly.detected`, `recommendation.created`, `budget.threshold`, `report.ready`, `connector.failed`, signés par HMAC avec le secret du webhook.
- **Exports planifiés** : lignes de coût CSV ou Parquet déposées quotidiennement ou mensuellement sur un stockage S3-compatible de l'organisation.

## CLI `kairn`

```bash
kairn login --url https://kairn.example.com --token kairn_…
kairn orgs && kairn use <org>
kairn summary
kairn costs --from 2026-09-01 --group-by provider,cost_type
kairn recommendations --status open
kairn recommendations accept <id>
kairn export costs --from 2026-08-01 --to 2026-09-01 > costs.csv
kairn ask "Pourquoi les coûts de l'équipe Data ont-ils augmenté ?"
```

Variables : `KAIRN_URL`, `KAIRN_TOKEN`, `KAIRN_ORG`, `KAIRN_CONFIG`.

## Terraform / OpenTofu

Le provider [`terraform-provider/`](../../terraform-provider/README.md) gère la configuration d'une organisation « as code » : `kairn_organization` (paramètres), `kairn_connector` (secrets en écriture seule), `kairn_allocation_node`, `kairn_allocation_rule`, `kairn_budget`, tous importables.

```hcl
provider "kairn" {} # KAIRN_URL, KAIRN_TOKEN

resource "kairn_budget" "data" {
  name   = "Équipe Data — mensuel"
  period = "monthly"
  amount = "8000.00"
}
```

## Serveur MCP

Les agents IA des clients (Claude, etc.) interrogent Kairn via le serveur **MCP** exposé sur `https://<kairn>/mcp` (transport HTTP « streamable »), authentifié par un jeton d'API Kairn. Chaque appel d'outil est rejoué sur l'API publique avec ce jeton : l'agent a exactement les droits de l'utilisateur, et aucun appel LLM n'est fait côté Kairn.

Outils (lecture seule) : `get_cost_summary`, `get_costs`, `get_usage`, `get_efficiency`, `list_recommendations`, `get_anomalies`, `get_events`, `get_budget_status`, `get_forecast`, `search_resources`, `list_allocation_nodes`, `render_chart`.

Exemple de déclaration côté client MCP :

```json
{
  "mcpServers": {
    "kairn": {
      "type": "http",
      "url": "https://kairn.example.com/mcp",
      "headers": { "Authorization": "Bearer kairn_…" }
    }
  }
}
```
