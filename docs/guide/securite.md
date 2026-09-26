# Sécurité

## Isolation des organisations

Deux couches indépendantes ([ADR-0002](../adr/0002-isolation-multi-tenant-rls.md)) : filtre `org_id` explicite dans chaque requête, et Row-Level Security PostgreSQL (`FORCE`) avec un rôle applicatif sans `BYPASSRLS`. ClickHouse, le stockage objet et le bus portent l'organisation dans chaque clé. Un test automatique appelle **chaque** opération de l'API avec les identifiants d'une autre organisation.

## Credentials des clouds

- Chiffrement **enveloppe** : une clé de données par secret, elle-même chiffrée par la clé maître (`KAIRN_KEK`) ou par Vault Transit / le KMS de l'hébergeur. Les secrets ne sont jamais renvoyés par l'API (écriture seule) ni journalisés.
- Les connecteurs sont en **lecture seule** ; chaque page de connecteur documente les [permissions minimales](../connectors/README.md) à accorder.
- Les webhooks entrants sont authentifiés (signature HMAC ou jeton selon la source) en plus du jeton secret de leur URL, réservée aux membres autorisés et régénérable (rotation).

## Identités et accès

- **SSO OIDC** (Keycloak fourni, ou tout fournisseur OIDC) ; SAML, MFA et fédération d'annuaires via Keycloak ; provisionnement **SCIM 2.0** (`/scim/v2`, jeton dédié).
- **RBAC** par organisation :

| Rôle | Droits |
|---|---|
| Owner | Tout, dont facturation et suppression de l'organisation |
| Admin | Tout sauf la facturation |
| Finance | Lecture + grilles, allocation, budgets, alertes, rapports, exports, assistant |
| Engineer | Lecture + recommandations, alertes, uptime, exports, assistant |
| Viewer | Lecture + assistant |

- **Scopes d'allocation** : un membre peut être restreint à un ou plusieurs nœuds de la hiérarchie (une équipe ne voit que ses coûts).
- **Jetons d'API** : préfixe `kairn_`, stockés hachés, limités à un rôle et optionnellement à des permissions (`costs:read`, `export`…), avec expiration.
- **Mode MSP** : une organisation parente gère des organisations clientes ; les accès restent cloisonnés par organisation.
- Sessions en cookie `HttpOnly`, `Secure`, `SameSite` ; en-têtes de sécurité (`X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy`, `Permissions-Policy`).

## Journal d'audit

Toute action d'écriture (connecteurs, règles, membres, jetons, paramètres, suppression) est journalisée avec l'acteur, l'IP cliente et la cible, en **ajout seul** (le rôle applicatif ne peut ni modifier ni supprimer une entrée). Export CSV / JSON.

## Protection de l'API

- Limitation de débit par principal ou par IP, partagée entre réplicas via Valkey/Redis ([ADR-0012](../adr/0012-limitation-de-debit-partagee.md)).
- IP cliente fiable : `X-Forwarded-For` n'est accepté que des proxys de confiance ([ADR-0011](../adr/0011-ip-cliente-et-proxys-de-confiance.md)).
- Erreurs normalisées RFC 9457 sans détail interne ; limites de plan appliquées côté API.
- Sondes d'uptime et appels sortants configurables protégés contre le SSRF (adresses privées, loopback, link-local et CGNAT refusées sauf `KAIRN_PROBE_ALLOW_PRIVATE` en self-hosted).

## IA

L'assistant et le serveur MCP n'accèdent aux données que par l'API publique avec le jeton de l'utilisateur : jamais plus de droits que lui ([ADR-0009](../adr/0009-ia-outils-types-et-ancrage-des-chiffres.md)). Aucun appel LLM hors UE sans accord explicite de l'organisation ; option souveraine (Mistral) ou modèle local.

## RGPD

- Hébergement UE, DPA, registre des traitements.
- **Droit à l'effacement** : la suppression d'une organisation (propriétaire, confirmation par slug) purge PostgreSQL, ClickHouse et le stockage objet.
- Les exports de données restent sous le contrôle de l'organisation (stockage S3-compatible de son choix).

## Chaîne logicielle

CI : `golangci-lint` (dont `gosec`), `ruff` (règles de sécurité), `mypy --strict`, analyse des dépendances, images multi-architectures avec SBOM et signature cosign. Pentest avant la disponibilité générale ; trajectoire ISO 27001, OWASP ASVS niveau 2.
