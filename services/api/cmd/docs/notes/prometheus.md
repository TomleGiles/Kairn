# Connecteur Prometheus / VictoriaMetrics / Thanos

Kairn consomme la pile de métriques existante plutôt que d'en déployer une : le connecteur interroge l'API HTTP de Prometheus (ou d'un système compatible : VictoriaMetrics, Thanos Query, Mimir/Cortex) et rattache les séries aux ressources d'un autre connecteur (`target_connector_id`).

## Modes

- **`kubernetes`** (défaut) : utilisation CPU et mémoire des pods et nodes à partir de cAdvisor (`container_cpu_usage_seconds_total`, `container_memory_working_set_bytes`) et de kube-state-metrics (`kube_pod_container_resource_requests`, `kube_node_status_capacity`). La cible est un connecteur [Kubernetes](kubernetes.md) ; utiliser `matchers` (ex. `cluster="prod-gra"`) pour un Prometheus multi-clusters.
- **`node`** : utilisation des VM à partir de node_exporter ; le label `vm_id_label` (défaut `instance_id`) doit porter l'identifiant de la VM chez le fournisseur. La cible est un connecteur cloud ([OpenStack](openstack.md), [OVHcloud](ovh.md)…) ou l'[agent](agent.md).

## Accès

Un jeton en lecture seule (`token`), une authentification basique (`username`/`password`) ou un accès réseau restreint suffisent. `tenant_id` positionne l'en-tête `X-Scope-OrgID` (Mimir/Cortex). Seules les routes `query` et `query_range` sont appelées.

## Rétention

Les points sont agrégés par Kairn (brut 15 jours, 5 min 90 jours, 1 h 25 mois) ; le backfill initial rejoue jusqu'à 13 mois si Prometheus les conserve.
