# Connecteur OpenStack

## Ce qui est collecté

- **Inventaire** (Nova, Cinder, Neutron, Octavia, Swift) : projet, instances (flavor, vCPU, RAM, état), volumes (type, taille, attachement), instantanés, IP flottantes, répartiteurs de charge, conteneurs Swift. Chaque ressource est historisée (qui existait quand, avec quelle taille).
- **Utilisation** (Gnocchi / Ceilometer, si présent) : CPU et mémoire des instances. Sans Gnocchi, associer un connecteur [Prometheus](prometheus.md) en mode `node` ou déployer l'[agent Kairn](agent.md).
- **Coût** : estimé à partir de la grille tarifaire du fournisseur (`pricing_provider`), par flavor, par Go de volume, par IP… Pour OVHcloud Public Cloud, renseigner `pricing_provider = ovh` pour appliquer la grille publique OVHcloud importée chaque jour ; pour un cloud privé, définir un modèle de coût on-prem (amortissement, énergie, licences) dans *Tarification*.

## Créer un accès en lecture seule

Recommandé : des **identifiants d'application** Keystone limités au rôle `reader` du projet.

```bash
openstack application credential create kairn \
  --role reader --description "Kairn (lecture seule)"
```

Saisir l'`id` et le `secret` retournés dans `application_credential_id` et `application_credential_secret`. Ajouter le rôle `load-balancer_observer` pour Octavia si les politiques du cloud l'exigent.

À défaut, un utilisateur dédié au rôle `reader` (`username`, `password`, domaines, projet) fonctionne aussi. Kairn vérifie l'accès et les permissions à l'enregistrement (`Validate`).

## Particularités

- `stopped_billing` : la plupart des clouds (dont OVHcloud) facturent une instance arrêtée (`SHUTOFF`) ; choisir `unbilled` si votre cloud privé ne la compte pas. Les instances `SHELVED_OFFLOADED` ne sont jamais facturées.
- `ca_cert` : certificat PEM de l'autorité interne d'un cloud privé ; la vérification TLS n'est jamais désactivée.
- Plusieurs régions ou projets : créer un connecteur par couple projet/région.
