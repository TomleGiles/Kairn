# Connecteur Scaleway

## Ce qui est collecté

- **Inventaire** par zone (`zones`) : instances (type commercial, état), volumes (`l_ssd`, `b_ssd`, `sbs_5k`, `sbs_15k`), instantanés, IP flexibles, répartiteurs de charge et clusters Kapsule/Kosmos (offre du plan de contrôle : `free`, `dedicated-4/8/16`, `multicloud`…).
- **Facturation réelle** : consommation facturée (API Billing) si la clé dispose de `BillingReadOnly`.
- **Tarifs publics** : la grille Scaleway est importée chaque jour du catalogue de produits public (prix par zone), sans credential. Voir [ADR-0006](../adr/0006-grilles-tarifaires-publiques.md).

## Créer un accès en lecture seule

Dans la console Scaleway (IAM), créer une **application** dédiée, lui attacher une politique avec les jeux de permissions `InstancesReadOnly`, `BlockStorageReadOnly` et, selon les besoins, `LoadBalancersReadOnly`, `KubernetesReadOnly`, `BillingReadOnly`, puis générer une clé d'API pour cette application. Saisir la clé secrète et l'identifiant d'organisation.

L'utilisation (CPU, mémoire) n'est pas exposée par ces API : utiliser [Prometheus](prometheus.md) ou l'[agent Kairn](agent.md).
