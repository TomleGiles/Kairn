# Connecteur Kubernetes

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
