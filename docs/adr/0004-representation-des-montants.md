# 0004 — Représentation des montants

- Statut : acceptée
- Date : 2026-09-26
- Modules : M-03, M-04, M-08, M-09, M-10

## Contexte

Les chiffres de Kairn servent à refacturer (chargeback) et à piloter des budgets ; l'écart estimé/facturé visé est < 2 %, et tout montant doit être traçable jusqu'à sa source. Les flottants binaires introduisent des erreurs d'arrondi cumulatives et non reproductibles.

## Décision

- **Jamais de float pour l'argent** : Go `shopspring/decimal` (type `money.Amount` = valeur + devise ISO 4217), Python `Decimal`, ClickHouse `Decimal(18, 6)`, PostgreSQL `numeric(18, 6)`, JSON **chaîne** (`"12.340000"`).
- **Précision** : 6 décimales en stockage (`money.StoragePlaces`), 2 à l'affichage (`money.DisplayPlaces`). Les prix unitaires trop fins (prix par Go-heure) sont convertis en prix par Go-mois à l'import (ADR-0006) plutôt que tronqués.
- **Convention horaire** : un mois de facturation = 730 h (`money.HoursPerMonth`), comme chez les fournisseurs.
- **Devises** : EUR par défaut ; toute opération entre devises différentes échoue (`ErrCurrencyMismatch`) ; la conversion passe par des taux journaliers versionnés (`exchange_rates`), jamais implicitement.
- **Traçabilité** : chaque ligne de coût porte `source` (estimation, facture, on-prem, allocation), `catalog_version` (`fournisseur@version`) et `source_ref`.
- Les métriques d'utilisation (CPU, octets…) restent en `Float64` : ce ne sont pas des montants.

## Conséquences

- Tests « golden » du cost-engine au centime (`pkg/costengine/golden_test.go`).
- Le front reçoit des chaînes et ne les convertit que pour l'affichage (`formatMoney`) ; aucun calcul monétaire n'est fait côté navigateur.
- L'assistant IA ne cite que des montants renvoyés par les outils (ADR-0009).
