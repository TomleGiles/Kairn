# Connecteur Prometheus / VictoriaMetrics / Thanos

> Page générée par `make gen` à partir des déclarations des connecteurs. Pour la modifier, éditer `services/api/cmd/docs/notes/prometheus.md` ou le connecteur.

| | |
|---|---|
| Type | `prometheus` |
| Nom | Prometheus / VictoriaMetrics / Thanos |
| Catégorie | metrics |
| Fournisseur | `prometheus` |
| Fréquence de synchronisation | 15 min |
| Ressources | — |
| Métriques d'utilisation | oui |
| Facturation réelle | non |
| Webhook entrant | non |

## Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| GET /api/v1/query_range, /api/v1/query | Lecture des séries (jeton en lecture seule ou accès réseau restreint). | oui |
| cAdvisor + kube-state-metrics | Mode kubernetes : container_cpu_usage_seconds_total, container_memory_working_set_bytes, kube_pod_container_resource_requests, kube_node_status_capacity. | oui |
| node_exporter | Mode node : node_cpu_seconds_total, node_memory_*, node_network_* avec un label identifiant la VM. | non |

## Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `url` | URL de l'API Prometheus | oui | non |  | ex. https://prometheus.example/ ou …/select/0/prometheus (VictoriaMetrics) |
| `target_connector_id` | Connecteur des ressources | oui | non |  | Connecteur Kubernetes (mode kubernetes) ou OpenStack/agent (mode node) |
| `mode` | Mode | non | non | `kubernetes` | kubernetes (cAdvisor + kube-state-metrics) ou node (node_exporter) |
| `matchers` | Filtre de séries | non | non |  | ex. cluster="prod-gra" pour un Prometheus multi-clusters |
| `vm_id_label` | Label de l'identifiant de VM (mode node) | non | non | `instance_id` |  |
| `step` | Pas d'échantillonnage | non | non | `5m` |  |
| `token` | Jeton (Bearer) | non | oui |  |  |
| `username` | Utilisateur (authentification basique) | non | non |  |  |
| `password` | Mot de passe | non | oui |  |  |
| `tenant_id` | Tenant (X-Scope-OrgID, Mimir/Cortex) | non | non |  |  |
| `ca_cert` | Certificat d'autorité (PEM) | non | non |  |  |

Kairn consomme la pile de métriques existante plutôt que d'en déployer une : le connecteur interroge l'API HTTP de Prometheus (ou d'un système compatible : VictoriaMetrics, Thanos Query, Mimir/Cortex) et rattache les séries aux ressources d'un autre connecteur (`target_connector_id`).

## Modes

- **`kubernetes`** (défaut) : utilisation CPU et mémoire des pods et nodes à partir de cAdvisor (`container_cpu_usage_seconds_total`, `container_memory_working_set_bytes`) et de kube-state-metrics (`kube_pod_container_resource_requests`, `kube_node_status_capacity`). La cible est un connecteur [Kubernetes](kubernetes.md) ; utiliser `matchers` (ex. `cluster="prod-gra"`) pour un Prometheus multi-clusters.
- **`node`** : utilisation des VM à partir de node_exporter ; le label `vm_id_label` (défaut `instance_id`) doit porter l'identifiant de la VM chez le fournisseur. La cible est un connecteur cloud ([OpenStack](openstack.md), [OVHcloud](ovh.md)…) ou l'[agent](agent.md).

## Accès

Un jeton en lecture seule (`token`), une authentification basique (`username`/`password`) ou un accès réseau restreint suffisent. `tenant_id` positionne l'en-tête `X-Scope-OrgID` (Mimir/Cortex). Seules les routes `query` et `query_range` sont appelées.

## Rétention

Les points sont agrégés par Kairn (brut 15 jours, 5 min 90 jours, 1 h 25 mois) ; le backfill initial rejoue jusqu'à 13 mois si Prometheus les conserve.
