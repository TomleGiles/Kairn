# Connecteur OVHcloud Public Cloud

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
