# 0006 — Import et versionnement des grilles tarifaires publiques

- Statut : acceptée
- Date : 2026-09-26
- Modules : M-03

## Contexte

Le coût estimé d'une ressource vient d'une grille tarifaire (`price_catalogs` / `price_items`). M-03 exige des grilles publiques **importées et versionnées** (OVHcloud, Scaleway, Outscale…) et un cost-engine **déterministe** : mêmes entrées + même version de grille = même résultat. Des grilles d'exemple indicatives sont embarquées pour la démo et l'installation initiale ; elles ne doivent jamais se mélanger à des prix officiels.

## Décision

**Sources** (API publiques, sans credential ni donnée client) :

| Fournisseur | Source | Particularités |
|---|---|---|
| OVHcloud | `GET /1.0/order/catalog/public/cloud?ovhSubsidiary=FR` | Prix en 10⁻⁸ de la devise ; offres « à la consommation » de la zone de référence ; déclinaisons 3AZ / Local Zones et forfaits mensuels ignorés |
| Scaleway | `GET /product-catalog/v2alpha1/public-catalog/products` (paginé) | Prix `units + nanos` par zone ; offres disponibles préférées aux offres retirées |
| OUTSCALE | `POST /api/v1/ReadPublicCatalog` par région | VM Tina tarifées par vCore selon génération/performance (`compute.vcpu.v6-p2`) + Go de RAM ; le connecteur pose `cpu_class` |

Chaque importeur (`connectors/<fournisseur>/pricing.go`) implémente `catalogs.Importer` et convertit le catalogue en SKU Kairn (`compute.flavor.<flavor>`, `storage.volume.<type>`, `network.lb.<offre>`, `k8s.control_plane.<offre>`…), les mêmes que ceux produits par les connecteurs d'inventaire. Les prix par Go-heure sont convertis en Go-mois (× 730) pour tenir dans la précision stockée (ADR-0004).

**Versionnement** : la version est l'empreinte SHA-256 du contenu normalisé (`pub-<12 hex>`) ; un contenu inchangé ne crée pas de version, l'import est donc idempotent (plusieurs instances, rejeux). Une nouvelle version prend effet **le lendemain (UTC)** de sa détection : une journée est toujours tarifée par une seule version. La **première** grille officielle d'un fournisseur prend effet au 1ᵉʳ janvier 2000 : faute d'historique officiel, elle s'applique aussi au passé (backfill) ; l'ordonnanceur relance alors le calcul du mois précédent et du mois courant.

**Priorité à la recherche d'un prix** (`pricing.Book`) : grille négociée de l'organisation, puis grille publique la plus récente valide à la date ; une grille plus ancienne complète la plus récente (SKU retirés encore facturés). Les grilles d'exemple (`source = sample`) sont **écartées** dès qu'une grille officielle est valide : un prix indicatif ne complète jamais une grille officielle.

**Exécution** : import quotidien par l'ordonnanceur (`KAIRN_PRICE_IMPORT`, défaut `ovh,scaleway,outscale` ; `none` pour une installation sans accès sortant) et à la demande (`kairn-api import-prices`). Sans accès sortant, les grilles s'importent par l'API (`POST /pricing/catalogs`, format JSON d'échange).

## Conséquences

- Chaque ligne de coût référence `fournisseur@pub-<empreinte>` : le prix appliqué est retrouvable.
- Les importeurs sont testés sur des extraits réels des trois catalogues (`connectors/*/testdata`) ; un changement de format fournisseur casse l'import (erreur journalisée, grille précédente conservée) plutôt que d'importer une grille vide (refusée).
- Limites connues : prix HT publics uniquement (remises via ajustements ou grilles négociées) ; OVHcloud 3AZ / Local Zones non distingués ; les prix Scaleway par zone supposent que le connecteur renseigne la zone.

## Alternatives écartées

- *Version datée (`2026-09-26`)* : crée une version par jour même sans changement et casse l'idempotence entre instances.
- *Appliquer chaque nouvelle version au passé* : rendrait les coûts historiques instables à chaque variation de prix.
