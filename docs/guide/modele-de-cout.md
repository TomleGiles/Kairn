# Modèle de coût

Chaque montant affiché par Kairn est **traçable** jusqu'à sa source : ligne de facture, grille tarifaire versionnée ou modèle on-prem, et métrique d'utilisation. Le calcul est **déterministe et rejouable** : mêmes entrées et même version de grille donnent le même résultat, au centime (tests « golden »).

## Calcul d'une journée

Le cost-engine (`pkg/costengine`) calcule une journée UTC d'une organisation, puis la réécrit entièrement (ADR-0005) :

1. **Tarification des ressources** : pour chaque version d'une ressource existant ce jour-là (l'inventaire est historisé : un redimensionnement en cours de journée donne deux versions), les composants tarifés sont multipliés par la durée d'existence :
   - instances : flavor (`compute.flavor.<flavor>`), sinon vCPU + Go de RAM (prix par classe de CPU quand la grille le prévoit, ex. OUTSCALE) ; licences ; une instance arrêtée est facturée ou non selon le fournisseur (`billing_state`) ;
   - volumes (`storage.volume.<type>`), instantanés, stockage objet (volumétrie mesurée), IP, répartiteurs de charge, bases managées, plans de contrôle Kubernetes ;
   - **on-prem** : un modèle de coût (matériel amorti, énergie × PUE, licences, main-d'œuvre, autres) est ventilé en coût par vCPU-heure, Go de RAM-heure et Go de stockage-mois, selon des pondérations et une capacité déclarées ; il s'applique par connecteur ou par sélecteur de labels.
2. **Kubernetes** : le coût de chaque node est réparti heure par heure entre ses pods selon `max(requests, usage)` (ou `requests`, `usage`) ; le reste est le **coût idle** ([ADR-0010](../adr/0010-repartition-des-couts-kubernetes.md)).
3. **Préférence facture** (option de l'organisation) : quand le fournisseur remonte la facture d'une ressource, elle remplace l'estimation.
4. **Allocation** : chaque ligne est attribuée à un nœud de la hiérarchie (organisation → business unit → équipe → service → environnement) par les règles d'allocation (labels/tags, projets, namespaces, expressions régulières, mapping manuel), dans l'ordre de priorité ; les labels sont hérités des parents (projet → VM → volume, workload → pod). Les coûts non attribués vont au nœud « Non alloué » : le **taux de couverture** est affiché.
5. **Coûts partagés** : redistribution des coûts d'un nœud (control plane, monitoring, ingress…) vers des nœuds cibles — proportionnelle aux coûts directs, fixe ou pondérée.
6. **Ajustements** : remises (pourcentage sur un périmètre), engagements (forfait mensuel couvrant un volume horaire d'un SKU : la consommation couverte est remplacée par le forfait réparti au prorata, la part inutilisée reste visible en non alloué, le dépassement au tarif normal), crédits (consommés au prorata des lignes concernées jusqu'à épuisement), puis TVA optionnelle.

Les montants sont des décimaux exacts à 6 décimales ; toute ligne porte sa devise, sa source (`estimate`, `invoice`, `onprem`…), la version de grille (`ovh@pub-3f2a…`) et une référence (ligne de facture, règle appliquée) ([ADR-0004](../adr/0004-representation-des-montants.md)).

## Grilles tarifaires

- **Grilles publiques** OVHcloud, Scaleway et OUTSCALE importées chaque jour de leurs catalogues publics et versionnées par empreinte de contenu ; une nouvelle version s'applique à partir du lendemain ([ADR-0006](../adr/0006-grilles-tarifaires-publiques.md)).
- **Grilles négociées** : une organisation peut importer sa propre grille (format JSON d'échange, `POST /pricing/catalogs`) ; elle est prioritaire sur la grille publique du même fournisseur.
- **Grilles d'exemple** : indicatives, pour la démo ; ignorées dès qu'une grille officielle existe.
- Devises : EUR par défaut ; conversion par taux journaliers versionnés.

## Estimé et facturé

Quand la facture est disponible (OVHcloud, Scaleway, OUTSCALE, exports FOCUS), Kairn rapproche l'estimation et la facture par fournisseur et par mois et affiche l'écart (objectif < 2 %). Un écart important signale une grille à compléter (SKU manquant, remise non déclarée) ; les avertissements du calcul (« aucun prix pour la flavor X ») sont visibles dans l'interface.

## Recalcul

Le calcul horaire couvre la veille et le jour en cours ; les changements de règles d'allocation, d'ajustements ou de grilles peuvent être appliqués au passé par `POST /api/v1/orgs/{org}/costs/recompute` (période au choix). Le résultat remplace les lignes existantes, sans doublon.

## Prévisions et simulations

- **Prévision** de fin de période par nœud (Holt-Winters amorti à saisonnalité hebdomadaire), avec intervalle de confiance, utilisée par les budgets (alerte sur prévision).
- **What-if** : ajout de nodes, changement de flavor, migration vers un autre fournisseur, chiffrés avec les mêmes grilles.
