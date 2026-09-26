# Connecteur Kubernetes

> Page générée par `make gen` à partir des déclarations des connecteurs. Pour la modifier, éditer `services/api/cmd/docs/notes/kubernetes.md` ou le connecteur.

| | |
|---|---|
| Type | `kubernetes` |
| Nom | Kubernetes (on-prem, OVHcloud MKS, Scaleway Kapsule…) |
| Catégorie | kubernetes |
| Fournisseur | `kubernetes` |
| Fréquence de synchronisation | 15 min |
| Ressources | `k8s.cluster`, `k8s.node`, `k8s.namespace`, `k8s.workload`, `k8s.pod`, `k8s.pvc` |
| Métriques d'utilisation | oui |
| Facturation réelle | non |
| Webhook entrant | non |

## Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| nodes, namespaces, pods, persistentvolumeclaims, events: get, list | Inventaire et événements du cluster. | oui |
| apps/deployments, statefulsets, daemonsets, replicasets: get, list | Workloads et rattachement des pods. | oui |
| batch/jobs, cronjobs: get, list | Rattachement des pods de jobs. | non |
| autoscaling/horizontalpodautoscalers: get, list | Bornes d'autoscaling des workloads. | non |
| metrics.k8s.io/pods, nodes: get, list | Utilisation instantanée (metrics-server). | non |

## Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `cluster_name` | Nom du cluster | oui | non |  | Identifiant stable, repris par le connecteur Prometheus |
| `api_server` | URL de l'API server | non | non |  | ex. https://xxxx.c1.gra7.k8s.ovh.net ; vide = dans le cluster |
| `token` | Jeton du compte de service (lecture seule) | non | oui |  |  |
| `ca_cert` | Certificat d'autorité du cluster (PEM) | non | non |  |  |
| `control_plane_tier` | Offre du plan de contrôle | non | non | `standard` | free, standard… (grille tarifaire kubernetes) |
| `event_reasons` | Événements suivis | non | non |  | Raisons séparées par des virgules ; vide = avertissements et mises à l'échelle |

## Ce qui est collecté

- **Inventaire** : cluster, nodes (capacité CPU/mémoire, instance sous-jacente), namespaces, workloads (Deployment, StatefulSet, DaemonSet, Job, CronJob), pods (requests/limits), PVC ; labels et annotations deviennent des labels Kairn (allocation par équipe, service, environnement).
- **Événements** : avertissements et mises à l'échelle (HPA, `ScalingReplicaSet`…), utilisés par la corrélation des anomalies (M-07). `event_reasons` permet de choisir les raisons suivies.
- **Utilisation instantanée** : metrics-server (`metrics.k8s.io`), si disponible. Pour l'historique d'utilisation (P95, rightsizing), associer un connecteur [Prometheus](prometheus.md) en mode `kubernetes` qui reprend le même `cluster_name`.

## Coût

Le coût de chaque node (flavor de la VM sous-jacente, ou modèle on-prem) est réparti heure par heure entre ses pods selon **max(requests, usage)** — méthode configurable dans les paramètres de l'organisation (`requests`, `usage`). La capacité non réservée ni utilisée devient le **coût idle** du node, conservé sur une ligne dédiée ou réparti au prorata. Les coûts partagés (control plane, monitoring, ingress) sont répartis par des règles de coûts partagés (M-04). Le plan de contrôle managé est tarifé selon `control_plane_tier` (ex. `free`, `standard` pour OVHcloud MKS).

## Créer un accès en lecture seule

Le manifeste [`deploy/kubernetes/kairn-reader.yaml`](../../deploy/kubernetes/kairn-reader.yaml) crée un compte de service `kairn-reader` avec un `ClusterRole` limité à `get`/`list` :

```bash
kubectl apply -f deploy/kubernetes/kairn-reader.yaml
kubectl -n kairn-system create token kairn-reader --duration=8760h
```

Saisir le jeton dans `token`, l'URL de l'API server dans `api_server` et le certificat d'autorité du cluster dans `ca_cert`. Aucun droit d'écriture n'est demandé.
