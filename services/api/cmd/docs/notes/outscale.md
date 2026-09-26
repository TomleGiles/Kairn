# Connecteur 3DS OUTSCALE

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
