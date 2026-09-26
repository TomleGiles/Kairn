# Architecture Decision Records

Toute décision structurante (nouvelle dépendance, changement de modèle de coût, choix d'algorithme, contrat entre services) donne lieu à un ADR (CLAUDE.md §9). Un ADR n'est jamais réécrit après acceptation : une décision qui change fait l'objet d'un nouvel ADR qui *remplace* l'ancien.

| N° | Décision | Statut |
|---|---|---|
| [0001](0001-architecture-et-pile-technique.md) | Architecture et pile technique | Acceptée |
| [0002](0002-isolation-multi-tenant-rls.md) | Isolation multi-tenant : filtre `org_id` + Row-Level Security | Acceptée |
| [0003](0003-erreurs-de-flux-des-connecteurs.md) | Erreurs en cours de flux des connecteurs | Acceptée |
| [0004](0004-representation-des-montants.md) | Représentation des montants | Acceptée |
| [0005](0005-agregats-clickhouse-rejouables.md) | Agrégats ClickHouse rejouables | Acceptée |
| [0006](0006-grilles-tarifaires-publiques.md) | Import et versionnement des grilles tarifaires publiques | Acceptée |
| [0007](0007-hyperscalers-via-focus.md) | Hyperscalers via les exports FOCUS | Acceptée |
| [0008](0008-mode-mono-noeud-et-demo.md) | Mode mono-nœud et mode démo | Acceptée |
| [0009](0009-ia-outils-types-et-ancrage-des-chiffres.md) | IA : outils typés sur l'API et ancrage des chiffres | Acceptée |
| [0010](0010-repartition-des-couts-kubernetes.md) | Répartition du coût des nodes Kubernetes | Acceptée |
| [0011](0011-ip-cliente-et-proxys-de-confiance.md) | IP cliente et proxys de confiance | Acceptée |
| [0012](0012-limitation-de-debit-partagee.md) | Limitation de débit partagée (Redis/Valkey) | Acceptée |

## Modèle

```markdown
# NNNN — Titre

- Statut : proposée | acceptée | remplacée par NNNN
- Date : AAAA-MM-JJ
- Modules : M-xx

## Contexte
## Décision
## Conséquences
## Alternatives écartées
```
