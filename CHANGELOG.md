# Journal des modifications

Format : [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/) ; versions : [SemVer](https://semver.org/lang/fr/). Chaque entrée référence son module (`M-xx`).

## [Non publié]

Première version complète du produit (périmètre des releases R1 à R5 de la feuille de route), non encore publiée.

### Ajouté

- **M-01 Connecteurs** : OpenStack (Keystone, Nova, Cinder, Neutron, Octavia, Swift, Gnocchi), OVHcloud (inventaire et consommation), Scaleway (dont volumes Block Storage SBS et offres Kapsule/Kosmos), OUTSCALE, Kubernetes (inventaire, événements, metrics-server), Prometheus/VictoriaMetrics/Thanos, exports FOCUS (AWS, Azure, GCP — ADR-0007), webhooks GitLab, GitHub, Argo CD, Flux, Alertmanager, PagerDuty, Opsgenie ; agent Kairn (Go, OTLP/HTTP) ; ingestion incrémentale, idempotente, backfill jusqu'à 13 mois ; erreurs de flux sans suppression fantôme (ADR-0003) ; santé et permissions minimales documentées.
- **M-02 Inventaire** : inventaire unifié historisé, graphe de relations, topologie, ressources orphelines.
- **M-03 Coûts** : cost-engine déterministe et rejouable (tests golden au centime) ; grilles publiques OVHcloud, Scaleway et OUTSCALE importées chaque jour et versionnées par empreinte (ADR-0006, `kairn-api import-prices`, `KAIRN_PRICE_IMPORT`) ; grilles négociées ; factures et rapprochement estimé/facturé ; coûts on-prem ; Kubernetes max(requests, usage), idle et coûts partagés (ADR-0010) ; tarification OUTSCALE par classe de vCore ; remises, engagements, crédits, TVA, devises ; montants décimaux exacts (ADR-0004).
- **M-04 Allocation** : hiérarchie organisation → business unit → équipe → service → environnement, règles (labels, projets, namespaces, regex), coûts partagés, showback et chargeback, taux de couverture.
- **M-05 Usage** : vues coût × utilisation, métriques unitaires, agrégats 5 min / 1 h rejouables (ADR-0005), sondes d'uptime multi-régions et pages de statut.
- **M-06 Recommandations** : rightsizing VM et workloads, orphelins, arrêts hors heures ouvrées, changement de gamme, stockage, engagements ; économie estimée puis mesurée, risque, preuves, commande prête à l'emploi, cycle de vie complet.
- **M-07 Anomalies** : détection saisonnière, corrélation avec déploiements, inventaire, HPA et incidents, explication fondée sur les preuves.
- **M-08 Budgets et alertes** : budgets à tous les niveaux, alertes sur réel et prévision, canaux e-mail, Slack, Teams, Mattermost, webhook, PagerDuty, silences, déduplication, heures ouvrées.
- **M-09 Prévisions** : Holt-Winters saisonnier avec intervalle de confiance, simulations what-if.
- **M-10 IA** : assistant ancré sur des outils typés appelant l'API avec les droits de l'utilisateur (ADR-0009), rapport mensuel exécutif (PDF + e-mail), serveur MCP ; fournisseurs Anthropic (repli serveur activé), Mistral, modèle local ; suivi du coût LLM et quotas.
- **M-11 Accès** : multi-tenant par filtre `org_id` et Row-Level Security (ADR-0002), RBAC, scopes d'allocation, OIDC (SAML et MFA via Keycloak), SCIM 2.0, mode MSP avec rapports mensuels en marque blanche (nom, couleur et contact du MSP, hérités par ses organisations clientes), journal d'audit en ajout seul, droit à l'effacement.
- **M-12 Intégrations** : API REST `/api/v1` (OpenAPI 3.1 générée, erreurs RFC 9457), exports CSV/Parquet/S3, webhooks sortants, CLI `kairn`, provider Terraform/OpenTofu (`kairn_organization`, `kairn_connector`, `kairn_allocation_node`, `kairn_allocation_rule`, `kairn_budget`).
- **M-13 SaaS** : onboarding guidé, plans et limites appliqués côté API, facturation Stripe.
- **Plateforme** : mode mono-nœud et mode démo (ADR-0008) ; limitation de débit partagée via Valkey/Redis (ADR-0012) ; IP cliente fiable derrière proxys de confiance (ADR-0011) ; OpenTelemetry.
- **Déploiement** : docker compose, Helm (distribué et mono-nœud), Argo CD, OpenTofu (OVHcloud) ; CI : lint, tests, intégration PostgreSQL/ClickHouse, évaluation IA, e2e, Helm, images multi-architectures avec SBOM et signature cosign.
- **Site vitrine** (`apps/site`) : page de présentation statique pour les acheteurs, optimisée pour le référencement (métadonnées, Open Graph, données structurées SoftwareApplication et FAQ, sitemap, robots), accessible (WCAG 2.1 AA), sans JavaScript client ni appel tiers ; `make site`.
- **Documentation** : guides, ADR 0001 à 0012, fiches connecteurs générées depuis le code, site servi sous `/docs`.
- **Tests** : unitaires et d'intégration (RLS réelle), isolation multi-tenant de chaque endpoint, Playwright (parcours et accessibilité WCAG 2.1 AA), charge k6.
