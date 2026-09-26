# 0010 — Répartition du coût des nodes Kubernetes

- Statut : acceptée
- Date : 2026-09-26
- Modules : M-03, M-04

## Contexte

Un cluster Kubernetes se paie par node (VM ou serveur), mais se consomme par pod. L'allocation par équipe/service (showback, chargeback) exige de répartir le coût des nodes entre les pods, de rendre visible la capacité payée et inutilisée (idle), et de conserver la somme : la somme des coûts répartis doit égaler le coût des nodes, au centime.

## Décision

- Le coût d'un node est celui de sa VM sous-jacente (relation `backs` : grille du fournisseur cloud, remises comprises) ou, on-prem, du modèle de coût appliqué ; la VM n'est alors pas facturée une seconde fois.
- Répartition **heure par heure** entre les pods présents sur le node, au prorata d'une part pondérée CPU / mémoire (poids issus du rapport de prix vCPU / Go de RAM de la flavor), selon la méthode de l'organisation :
  - `max` (défaut) : `max(requests, usage)` — une réservation non utilisée reste imputée, un dépassement aussi ;
  - `requests` : réservations seules ; `usage` : utilisation seule.
- La part non attribuée devient le **coût idle** du node : ligne dédiée (`keep`, défaut) ou redistribuée aux pods au prorata (`distribute`).
- Le plan de contrôle managé est tarifé séparément (`k8s.control_plane.<offre>`) ; les coûts partagés (control plane, monitoring, ingress) se répartissent par règles de coûts partagés (proportionnel, fixe, pondéré — M-04).
- Les lignes de pods portent `kairn.node_id`, `kairn.vm_id` et les SKU du node (`kairn.sku`) pour que remises et engagements s'appliquent aussi au coût réparti.

## Conséquences

- Conservation exacte vérifiée par les tests golden ; l'idle est un indicateur d'efficience actionnable (recommandations de rightsizing des requests, M-06).
- Sans métriques d'utilisation (pas de Prometheus / metrics-server), `max` se réduit à `requests`.

## Alternatives écartées

- *Requests seules par défaut* : ignore les pods qui dépassent massivement leurs requests (sous-facturation).
- *Répartition journalière* : fausse les coûts des pods éphémères (jobs, autoscaling).
