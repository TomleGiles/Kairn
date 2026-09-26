# Connecteur FOCUS (AWS, Azure, Google Cloud…)

> Page générée par `make gen` à partir des déclarations des connecteurs. Pour la modifier, éditer `services/api/cmd/docs/notes/focus.md` ou le connecteur.

| | |
|---|---|
| Type | `focus` |
| Nom | Export FOCUS (AWS, Azure, Google Cloud…) |
| Catégorie | billing |
| Fournisseur | `focus` |
| Fréquence de synchronisation | 6 h |
| Ressources | `service` |
| Métriques d'utilisation | non |
| Facturation réelle | oui |
| Webhook entrant | non |

## Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| s3:ListBucket, s3:GetObject sur le préfixe d'export | Lecture des fichiers FOCUS déposés par le fournisseur. | oui |

## Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `provider` | Fournisseur | non | non | `aws` | aws, azure, gcp… (libellé des coûts importés) |
| `source` | Emplacement | oui | non |  | s3://bucket/prefix ou https://…/export.csv.gz |
| `s3_endpoint` | Point d'accès S3 | non | non | `s3.amazonaws.com` | ex. s3.gra.io.cloud.ovh.net pour un bucket OVHcloud |
| `s3_region` | Région S3 | non | non | `eu-west-3` |  |
| `access_key` | Access key (lecture seule) | non | non |  |  |
| `secret_key` | Secret key | non | oui |  |  |
| `cost_column` | Colonne de coût | non | non | `EffectiveCost` | EffectiveCost (remises et engagements amortis) ou BilledCost |

Les hyperscalers sont un besoin secondaire de Kairn (environnements hybrides). Plutôt que trois connecteurs propriétaires, Kairn lit les exports de facturation au format ouvert **FinOps FOCUS** (FinOps Open Cost and Usage Specification, v1.x), que les trois fournisseurs savent produire. Voir [ADR-0007](../adr/0007-hyperscalers-via-focus.md).

## Mise en place

- **AWS** : *Billing and Cost Management → Data Exports*, type « FOCUS », livraison dans un bucket S3.
- **Azure** : *Cost Management → Exports*, format « FOCUS », vers un compte de stockage (exposé via une URL SAS HTTPS ou copié vers un stockage S3-compatible).
- **Google Cloud** : export BigQuery de la facturation, puis requête/vue FOCUS exportée en CSV vers un stockage.

Renseigner `source` (`s3://bucket/prefix` ou URL HTTPS d'un fichier `.csv` / `.csv.gz`) et, pour S3, une clé en lecture seule (`s3:ListBucket`, `s3:GetObject` sur le préfixe). `s3_endpoint` permet d'utiliser un stockage S3-compatible européen (ex. OVHcloud, Scaleway).

## Traitement

- La validation lit l'en-tête du premier fichier et vérifie les colonnes FOCUS obligatoires (`ChargePeriodStart`, colonne de coût, `BillingCurrency`).
- `cost_column` : `EffectiveCost` (remises et engagements amortis, recommandé) ou `BilledCost`.
- Chaque `ResourceId` facturé devient une ressource d'inventaire (les `Tags` FOCUS deviennent des labels, donc allouables) et chaque ligne une ligne de facture rattachée. Les montants sont lus en décimal exact, jamais en flottant.
