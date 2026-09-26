# Connecteur 3DS OUTSCALE

> Page générée par `make gen` à partir des déclarations des connecteurs. Pour la modifier, éditer `services/api/cmd/docs/notes/outscale.md` ou le connecteur.

| | |
|---|---|
| Type | `outscale` |
| Nom | 3DS OUTSCALE |
| Catégorie | cloud |
| Fournisseur | `outscale` |
| Fréquence de synchronisation | 1 h |
| Ressources | `compute.instance`, `storage.volume`, `storage.snapshot`, `network.ip`, `network.loadbalancer` |
| Métriques d'utilisation | non |
| Facturation réelle | oui |
| Webhook entrant | non |

## Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| api:Read* | Lecture des VM, volumes, instantanés, IP publiques et répartiteurs (politique EIM en lecture seule). | oui |
| api:ReadConsumptionAccount | Consommation et prix (rapprochement estimé / facturé). | non |

## Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `region` | Région | non | non | `eu-west-2` | eu-west-2, cloudgouv-eu-west-1 (SecNumCloud), us-east-2… |
| `access_key` | Access key | oui | non |  |  |
| `secret_key` | Secret key | oui | oui |  |  |

## Ce qui est collecté

- **Inventaire** (API OUTSCALE, signature OSC4-HMAC-SHA256) : VM (type, forme vCore/RAM, classe de CPU), volumes BSU, instantanés, IP publiques, répartiteurs LBU.
- **Consommation** (`ReadConsumptionAccount`, facultatif) pour le rapprochement estimé / facturé.
- **Tarifs publics** : la grille OUTSCALE est importée chaque jour par région via `ReadPublicCatalog` (sans credential). Les VM Tina sont tarifées **par vCore selon la génération et la performance** (`compute.vcpu.v6-p2`…) et par Go de RAM : le connecteur pose l'attribut `cpu_class` déduit du type `tinavG.cXrYpZ`. Voir [ADR-0006](../adr/0006-grilles-tarifaires-publiques.md).

## Région souveraine

La région `cloudgouv-eu-west-1` est qualifiée SecNumCloud ; renseigner `region` en conséquence. Un connecteur couvre une région.

## Créer un accès en lecture seule

Créer un utilisateur EIM dédié avec une politique n'autorisant que les appels de lecture :

```json
{"Statement": [{"Effect": "Allow", "Action": ["api:Read*"], "Resource": ["*"]}]}
```

puis générer une paire access key / secret key pour cet utilisateur. `api:ReadConsumptionAccount` est couvert par `api:Read*`.
