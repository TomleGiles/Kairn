# Connecteur OpenStack

> Page générée par `make gen` à partir des déclarations des connecteurs. Pour la modifier, éditer `services/api/cmd/docs/notes/openstack.md` ou le connecteur.

| | |
|---|---|
| Type | `openstack` |
| Nom | OpenStack (privé, OVHcloud Public Cloud, Infomaniak…) |
| Catégorie | cloud |
| Fournisseur | `openstack` |
| Fréquence de synchronisation | 1 h |
| Ressources | `project`, `compute.instance`, `storage.volume`, `storage.snapshot`, `network.ip`, `network.loadbalancer`, `storage.bucket` |
| Métriques d'utilisation | oui |
| Facturation réelle | non |
| Webhook entrant | non |

## Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| keystone: role reader sur le projet | Lecture des serveurs, volumes, IP flottantes et du catalogue (rôle « reader » des politiques par défaut). | oui |
| octavia: role load-balancer_observer | Lecture des répartiteurs de charge. | non |
| swift: lecture du compte | Liste des conteneurs et volumétrie du stockage objet. | non |
| gnocchi: lecture des métriques | Utilisation CPU et mémoire des instances (sinon, utiliser le connecteur Prometheus). | non |

## Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `auth_url` | URL Keystone v3 | oui | non |  | ex. https://auth.cloud.ovh.net/v3 |
| `region` | Région | non | non |  | ex. GRA11 ; vide = première région du catalogue |
| `application_credential_id` | ID des identifiants d'application | non | non |  | Recommandé : identifiants d'application avec le rôle reader |
| `application_credential_secret` | Secret des identifiants d'application | non | oui |  |  |
| `username` | Utilisateur (si pas d'identifiants d'application) | non | non |  |  |
| `password` | Mot de passe | non | oui |  |  |
| `user_domain_name` | Domaine de l'utilisateur | non | non | `Default` |  |
| `project_id` | ID du projet | non | non |  |  |
| `project_name` | Nom du projet (si pas d'ID) | non | non |  |  |
| `project_domain_name` | Domaine du projet | non | non | `Default` |  |
| `interface` | Interface des points d'accès | non | non | `public` |  |
| `stopped_billing` | Facturation des instances arrêtées | non | non | `billed` | billed (OVHcloud, la plupart des clouds) ou unbilled |
| `metrics` | Source d'utilisation | non | non | `auto` | auto (Gnocchi si présent), gnocchi ou none |
| `ca_cert` | Certificat d'autorité (PEM) | non | non |  | Pour un cloud privé à PKI interne |
| `pricing_provider` | Grille tarifaire | non | non |  | ex. ovh pour appliquer la grille OVHcloud ; vide = openstack |

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
