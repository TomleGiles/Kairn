# Connecteur OVHcloud Public Cloud

> Page générée par `make gen` à partir des déclarations des connecteurs. Pour la modifier, éditer `services/api/cmd/docs/notes/ovh.md` ou le connecteur.

| | |
|---|---|
| Type | `ovh` |
| Nom | OVHcloud Public Cloud |
| Catégorie | cloud |
| Fournisseur | `ovh` |
| Fréquence de synchronisation | 1 h |
| Ressources | `project`, `compute.instance`, `storage.volume`, `storage.snapshot`, `network.ip`, `network.loadbalancer`, `storage.bucket`, `k8s.cluster` |
| Métriques d'utilisation | non |
| Facturation réelle | oui |
| Webhook entrant | non |

## Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| GET /cloud/project/* | Inventaire des projets Public Cloud et consommation (usage/current, usage/history). | oui |
| GET /auth/time | Horloge de l'API pour la signature des requêtes. | oui |

## Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `endpoint` | Point d'accès | non | non | `ovh-eu` | ovh-eu, ovh-ca ou ovh-us |
| `application_key` | Application key | oui | non |  |  |
| `application_secret` | Application secret | oui | oui |  |  |
| `consumer_key` | Consumer key (GET /cloud/project/*) | oui | oui |  |  |
| `project_ids` | Projets | non | non |  | Identifiants séparés par des virgules ; vide = tous les projets accessibles |

## Ce qui est collecté

- **Inventaire** via l'API OVHcloud : projets Public Cloud, instances, volumes, instantanés, IP flottantes, répartiteurs de charge (Octavia), conteneurs de stockage objet et clusters Managed Kubernetes (MKS).
- **Facturation réelle** : consommation courante et historique (`usage/current`, `usage/history`) importée comme lignes de facture ; le cost-engine la rapproche de l'estimation (écart affiché, objectif < 2 %) et la préfère à l'estimation quand elle existe.
- **Tarifs publics** : la grille OVHcloud Public Cloud est importée chaque jour du catalogue de commande public, sans credential (voir [ADR-0006](../adr/0006-grilles-tarifaires-publiques.md)).

L'utilisation (CPU, mémoire) ne fait pas partie de l'API OVHcloud : utiliser le connecteur [OpenStack](openstack.md) avec Gnocchi, [Prometheus](prometheus.md) (node_exporter) ou l'[agent Kairn](agent.md).

## Créer un accès en lecture seule

1. Créer une application sur <https://eu.api.ovh.com/createApp/> (ou `ca.` / `us.` selon le point d'accès) : on obtient `application_key` et `application_secret`.
2. Demander une *consumer key* limitée à la lecture :

```bash
curl -X POST https://eu.api.ovh.com/1.0/auth/credential \
  -H "X-Ovh-Application: <application_key>" -H "Content-Type: application/json" \
  -d '{"accessRules":[{"method":"GET","path":"/cloud/project/*"},{"method":"GET","path":"/cloud/project"}]}'
```

3. Valider l'URL renvoyée (`validationUrl`) avec le compte OVHcloud, puis saisir la `consumerKey` dans Kairn.

Les requêtes sont signées (`$1$` SHA-1, horloge de `/auth/time`) ; aucune route d'écriture n'est autorisée par la consumer key.
