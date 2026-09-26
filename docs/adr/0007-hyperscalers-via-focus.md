# 0007 — Hyperscalers via les exports FOCUS

- Statut : acceptée
- Date : 2026-09-26
- Modules : M-01, M-03

## Contexte

AWS, Azure et Google Cloud sont des connecteurs **secondaires** (environnements hybrides, CLAUDE.md M-01) : Kairn se positionne sur les clouds souverains, OpenStack et Kubernetes. Trois connecteurs natifs (API de facturation, IAM, formats propres) représenteraient un coût de maintenance disproportionné.

## Décision

Un connecteur unique `focus` lit les exports de facturation au format **FinOps FOCUS** (v1.x), que les trois fournisseurs produisent nativement (AWS Data Exports, Azure Cost Management exports, Google Cloud via BigQuery) :

- source S3-compatible (`s3://bucket/prefix`, point d'accès configurable pour un stockage européen) ou URL HTTPS ; fichiers CSV éventuellement gzip ;
- validation par lecture de l'en-tête (colonnes obligatoires `ChargePeriodStart`, colonne de coût, `BillingCurrency`) ;
- `EffectiveCost` par défaut (remises et engagements amortis), `BilledCost` en option ;
- chaque `ResourceId` devient une ressource d'inventaire (tags FOCUS → labels, donc allouables), chaque ligne une ligne de facture (source « facture »).

## Conséquences

- Coûts hyperscalers disponibles pour l'allocation, les budgets, les anomalies et les rapports, sans inventaire temps réel ni métriques d'utilisation natives (utiliser Prometheus ou l'agent).
- Pas de recommandation de rightsizing spécifique aux hyperscalers dans cette version.
- Les montants sont lus en décimal exact ; les tests couvrent CSV et CSV gzip, S3 signé et HTTPS.

## Alternatives écartées

- *Connecteurs natifs AWS CUR / Azure / GCP* : meilleure fraîcheur, mais trois intégrations IAM et formats à maintenir pour un besoin secondaire. Restent possibles plus tard derrière la même interface `Connector`.
