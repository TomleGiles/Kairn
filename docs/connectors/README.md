# Connecteurs

> Page générée par `make gen` à partir des déclarations des connecteurs. Pour la modifier, éditer `services/api/cmd/docs` ou le connecteur.

Tous les connecteurs sont en **lecture seule** : ils n'effectuent aucune écriture sur l'infrastructure du client. Les credentials sont chiffrés (enveloppe, KMS ou Vault) et ne sont jamais journalisés.

| Connecteur | Catégorie | Fréquence | Métriques | Facturation réelle |
|---|---|---|---|---|
| [Agent Kairn (hôtes on-prem / VM)](agent.md) | agent | 5 min | oui | non |
| [Export FOCUS (AWS, Azure, Google Cloud…)](focus.md) | billing | 6 h | non | oui |
| [Kubernetes (on-prem, OVHcloud MKS, Scaleway Kapsule…)](kubernetes.md) | kubernetes | 15 min | oui | non |
| [OpenStack (privé, OVHcloud Public Cloud, Infomaniak…)](openstack.md) | cloud | 1 h | oui | non |
| [3DS OUTSCALE](outscale.md) | cloud | 1 h | non | oui |
| [OVHcloud Public Cloud](ovh.md) | cloud | 1 h | non | oui |
| [Prometheus / VictoriaMetrics / Thanos](prometheus.md) | metrics | 15 min | oui | non |
| [Scaleway](scaleway.md) | cloud | 1 h | non | oui |
| [Prometheus Alertmanager (incidents)](webhooks.md#alertmanager) | events | push | non | non |
| [Argo CD (synchronisations)](webhooks.md#argo-cd) | events | push | non | non |
| [Flux CD (réconciliations)](webhooks.md#flux) | events | push | non | non |
| [GitHub (déploiements)](webhooks.md#github) | events | push | non | non |
| [GitLab (déploiements)](webhooks.md#gitlab) | events | push | non | non |
| [Opsgenie (alertes)](webhooks.md#opsgenie) | events | push | non | non |
| [PagerDuty (incidents)](webhooks.md#pagerduty) | events | push | non | non |
