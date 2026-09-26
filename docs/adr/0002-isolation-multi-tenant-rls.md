# 0002 — Isolation multi-tenant : filtre `org_id` + Row-Level Security

- Statut : acceptée
- Date : 2026-09-26
- Modules : M-11, tous

## Contexte

Kairn héberge les coûts, l'inventaire et les credentials de nombreuses organisations dans les mêmes bases. Une fuite entre organisations est l'incident le plus grave possible. CLAUDE.md impose une isolation stricte par `org_id` : RLS PostgreSQL **et** vérification applicative, avec des tests d'isolation obligatoires.

## Décision

Deux couches indépendantes, chacune suffisante à elle seule :

1. **Applicative** : toute requête filtre explicitement sur `org_id`, tiré du contexte (`pkg/tenancy`) et jamais du corps de requête. Le contexte porte soit une organisation, soit le marqueur *système* (ordonnanceurs), jamais les deux. Le stockage en mémoire (`memstore`) applique les mêmes contrôles (`tenancy.Check`).
2. **Base** : chaque table client a `ENABLE` + `FORCE ROW LEVEL SECURITY` avec une politique `org_id = kairn_current_org()`. Chaque transaction positionne, localement (`set_config(..., true)`), `app.org_id`, `app.user_id` et `app.system`.

Rôles PostgreSQL (`deploy/postgres/init-roles.sql`) :

- `kairn_owner` : propriétaire du schéma, exécute les migrations, possède les fonctions `SECURITY DEFINER` ;
- `kairn_app` : rôle de connexion des services, **`NOBYPASSRLS`**, sans droit sur `schema_migrations`, `UPDATE`/`DELETE` révoqués sur `audit_events` (journal en ajout seul).

Les rares accès transverses (retrouver un utilisateur à la connexion, un jeton d'API par préfixe, un connecteur par jeton de webhook, une status page par slug, la liste des organisations pour l'ordonnanceur) passent **uniquement** par des fonctions `SECURITY DEFINER` étroites (`kairn_find_user`, `kairn_find_api_token`, `kairn_list_org_ids`…) qui ne renvoient que le strict nécessaire.

Les données ClickHouse portent `org_id` en tête de clé de tri ; toute requête `pkg/tsdb` exige l'organisation du contexte.

## Conséquences

- Une requête oubliant le filtre `org_id` ne renvoie rien d'une autre organisation (RLS) ; une RLS mal configurée est compensée par le filtre applicatif.
- Tests : la suite de contrat `pkg/store/storetest` s'exécute sur `memstore` et sur PostgreSQL réel avec le rôle `kairn_app` (`make test-int`) ; `TestTenantIsolationAllEndpoints` appelle **chaque** opération d'organisation de l'OpenAPI avec la session et le jeton d'une autre organisation et vérifie qu'aucune ne réussit ni ne divulgue d'identifiant.
- La suppression d'une organisation (propriétaire, confirmation par slug) purge PostgreSQL, ClickHouse et le stockage objet (droit à l'effacement RGPD).
- Le Helm chart refuse implicitement une mauvaise configuration : `postgres-url` doit utiliser le rôle applicatif, `postgres-migrate-url` le rôle propriétaire (voir `NOTES.txt`).

## Alternatives écartées

- *Une base ou un schéma par organisation* : isolation physique forte, mais coût opérationnel (migrations × N, connexions) incompatible avec un SaaS à nombreuses petites organisations. Reste possible en self-hosted (une instance par client).
- *RLS seule* : un rôle mal configuré (`BYPASSRLS`, superutilisateur) annulerait toute isolation sans signal.
