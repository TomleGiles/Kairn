# 0005 — Agrégats ClickHouse rejouables

- Statut : acceptée
- Date : 2026-09-26
- Modules : M-01, M-03, M-05

## Contexte

La rétention imposée (M-05) est : brut 15 jours, agrégé 5 min 90 jours, agrégé 1 h 25 mois. L'ingestion doit être **idempotente et rejouable** (backfill jusqu'à 13 mois, resynchronisation après incident). Les vues matérialisées incrémentales de ClickHouse agrègent chaque insertion : un point rejoué serait compté deux fois.

## Décision

- Tables en `ReplacingMergeTree` : `metrics_raw` (clé `org_id, resource_id, metric, ts`), `metrics_5m` et `metrics_1h` (versionnées par `version`), `cost_lines` (versionnée par `computed_at`), `billing_lines`, `events`. Réinsérer un point ou une ligne remplace l'existant au lieu de l'additionner.
- Les agrégats sont produits par des **tâches de rollup idempotentes** (`TSDB.Rollup`) : `INSERT … SELECT … FINAL` recalculant entièrement les buckets des heures terminées de la fenêtre (moyenne, min, max, P95, nombre d'échantillons). Elles sont lancées chaque heure sur les 3 dernières heures **et immédiatement après chaque synchronisation de métriques**, y compris chaque jour rejoué par un backfill : les points bruts de plus de 15 jours expirent (TTL), seuls les agrégats conservent l'historique.
- TTL par table selon la rétention M-05 ; les lignes de coût ne sont pas purgées (historique financier).
- Les lectures utilisent `FINAL` (ou des agrégats par clé) lorsque des doublons non fusionnés pourraient fausser le résultat.
- Le cost-engine recalcule une journée entière d'une organisation et la réécrit : le résultat est identique pour des entrées et une version de grille identiques.

## Conséquences

- Backfills et rejeux sont sûrs ; un calcul peut être relancé à tout moment (`POST /costs/recompute`).
- Coût : un rollup relit les points bruts de la fenêtre ; acceptable avec la granularité horaire et le partitionnement par jour.
- Les tests `pkg/tsdb/tsdbtest` (contrat commun mémoire / ClickHouse réel en intégration) vérifient notamment le remplacement d'une journée de coûts recalculée et l'isolation par organisation ; `TestBackfillRollsUpMetrics` vérifie que chaque jour rejoué est agrégé.

## Alternatives écartées

- *Vues matérialisées `AggregatingMergeTree`* : plus économes, mais non idempotentes face au rejeu.
- *TimescaleDB* : une seule base, mais performances d'agrégation inférieures sur 12 mois × 10 000 ressources.
