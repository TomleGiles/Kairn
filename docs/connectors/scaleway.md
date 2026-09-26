# Connecteur Scaleway

> Page générée par `make gen` à partir des déclarations des connecteurs. Pour la modifier, éditer `services/api/cmd/docs/notes/scaleway.md` ou le connecteur.

| | |
|---|---|
| Type | `scaleway` |
| Nom | Scaleway |
| Catégorie | cloud |
| Fournisseur | `scaleway` |
| Fréquence de synchronisation | 1 h |
| Ressources | `compute.instance`, `storage.volume`, `storage.snapshot`, `network.ip`, `network.loadbalancer`, `k8s.cluster` |
| Métriques d'utilisation | non |
| Facturation réelle | oui |
| Webhook entrant | non |

## Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| InstancesReadOnly, BlockStorageReadOnly | Instances, volumes, instantanés et IP flexibles. | oui |
| LoadBalancersReadOnly, KubernetesReadOnly | Répartiteurs de charge et clusters Kapsule. | non |
| BillingReadOnly | Consommation facturée (rapprochement estimé / facturé). | non |

## Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `secret_key` | Clé secrète d'API | oui | oui |  |  |
| `organization_id` | ID d'organisation | oui | non |  |  |
| `zones` | Zones | non | non | `fr-par-1,fr-par-2,nl-ams-1,pl-waw-1` |  |

## Ce qui est collecté

- **Inventaire** par zone (`zones`) : instances (type commercial, état), volumes (`l_ssd`, `b_ssd`, `sbs_5k`, `sbs_15k`), instantanés, IP flexibles, répartiteurs de charge et clusters Kapsule/Kosmos (offre du plan de contrôle : `free`, `dedicated-4/8/16`, `multicloud`…).
- **Facturation réelle** : consommation facturée (API Billing) si la clé dispose de `BillingReadOnly`.
- **Tarifs publics** : la grille Scaleway est importée chaque jour du catalogue de produits public (prix par zone), sans credential. Voir [ADR-0006](../adr/0006-grilles-tarifaires-publiques.md).

## Créer un accès en lecture seule

Dans la console Scaleway (IAM), créer une **application** dédiée, lui attacher une politique avec les jeux de permissions `InstancesReadOnly`, `BlockStorageReadOnly` et, selon les besoins, `LoadBalancersReadOnly`, `KubernetesReadOnly`, `BillingReadOnly`, puis générer une clé d'API pour cette application. Saisir la clé secrète et l'identifiant d'organisation.

L'utilisation (CPU, mémoire) n'est pas exposée par ces API : utiliser [Prometheus](prometheus.md) ou l'[agent Kairn](agent.md).
