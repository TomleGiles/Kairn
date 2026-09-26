# Connecteur FOCUS (AWS, Azure, Google Cloud…)

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
