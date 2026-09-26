# Webhooks de déploiement et d'incident

> Page générée par `make gen` à partir des déclarations des connecteurs. Pour la modifier, éditer `services/api/cmd/docs/notes/webhooks.md` ou le connecteur.

Ces sources alimentent la **corrélation des anomalies** (M-07) : quand un coût ou une utilisation dérive, Kairn recherche dans la fenêtre les déploiements, synchronisations, réconciliations et incidents reçus. Elles n'ont besoin d'**aucun accès** à l'outil source : c'est lui qui appelle l'URL fournie par Kairn.

Pour chaque source, créer le connecteur dans Kairn : l'URL de réception (`https://<kairn>/ingest/v1/webhooks/<jeton>`) figure dans la fiche du connecteur. Elle contient un jeton secret : elle n'est visible que des membres autorisés à lire les connecteurs et peut être régénérée (rotation) à tout moment. Chaque requête est authentifiée par le secret configuré (signature HMAC ou jeton selon la source) ; une requête non authentifiée est rejetée. `target_connector_id` rattache les événements aux ressources d'un connecteur Kubernetes ou cloud (par nom de service / namespace).

<a id="gitlab"></a>

## GitLab

*Settings → Webhooks* du projet : URL Kairn, **Secret token** = secret du connecteur (en-tête `X-Gitlab-Token`), cocher **Deployment events**.

| | |
|---|---|
| Type | `gitlab` |
| Nom | GitLab (déploiements) |
| Catégorie | events |
| Fournisseur | `gitlab` |
| Fréquence de synchronisation | — (réception en push) |
| Ressources | — |
| Métriques d'utilisation | non |
| Facturation réelle | non |
| Webhook entrant | oui |

### Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| webhook sortant | Aucun accès à la source : elle appelle l'URL fournie par Kairn. | oui |

### Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `webhook_secret` | Secret du webhook | oui | oui |  | Jeton secret du webhook GitLab (en-tête X-Gitlab-Token) ; cocher « Deployment events » |
| `target_connector_id` | Connecteur des ressources | non | non |  | Connecteur Kubernetes ou cloud auquel rattacher les événements |



<a id="github"></a>

## GitHub

*Settings → Webhooks* : URL Kairn, *Content type* `application/json`, **Secret** = secret du connecteur (signature `X-Hub-Signature-256`), événement **Deployment statuses**.

| | |
|---|---|
| Type | `github` |
| Nom | GitHub (déploiements) |
| Catégorie | events |
| Fournisseur | `github` |
| Fréquence de synchronisation | — (réception en push) |
| Ressources | — |
| Métriques d'utilisation | non |
| Facturation réelle | non |
| Webhook entrant | oui |

### Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| webhook sortant | Aucun accès à la source : elle appelle l'URL fournie par Kairn. | oui |

### Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `webhook_secret` | Secret du webhook | oui | oui |  | Secret du webhook GitHub (signature X-Hub-Signature-256) ; événement « Deployment statuses » |
| `target_connector_id` | Connecteur des ressources | non | non |  | Connecteur Kubernetes ou cloud auquel rattacher les événements |



<a id="argo-cd"></a>

## Argo CD

Dans `argocd-notifications-cm`, déclarer un service webhook vers l'URL Kairn avec l'en-tête `X-Webhook-Secret` et un déclencheur sur `on-sync-succeeded` / `on-sync-failed`.

| | |
|---|---|
| Type | `argocd` |
| Nom | Argo CD (synchronisations) |
| Catégorie | events |
| Fournisseur | `argocd` |
| Fréquence de synchronisation | — (réception en push) |
| Ressources | — |
| Métriques d'utilisation | non |
| Facturation réelle | non |
| Webhook entrant | oui |

### Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| webhook sortant | Aucun accès à la source : elle appelle l'URL fournie par Kairn. | oui |

### Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `webhook_secret` | Secret du webhook | oui | oui |  | Valeur de l'en-tête X-Webhook-Secret configuré dans argocd-notifications-cm |
| `target_connector_id` | Connecteur des ressources | non | non |  | Connecteur Kubernetes ou cloud auquel rattacher les événements |



<a id="flux"></a>

## Flux

Créer un `Provider` du notification-controller de type `generic-hmac` pointant vers l'URL Kairn, avec le secret du connecteur (en-tête `X-Signature`), et une `Alert` sur les `Kustomization` / `HelmRelease` suivies.

| | |
|---|---|
| Type | `flux` |
| Nom | Flux CD (réconciliations) |
| Catégorie | events |
| Fournisseur | `flux` |
| Fréquence de synchronisation | — (réception en push) |
| Ressources | — |
| Métriques d'utilisation | non |
| Facturation réelle | non |
| Webhook entrant | oui |

### Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| webhook sortant | Aucun accès à la source : elle appelle l'URL fournie par Kairn. | oui |

### Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `webhook_secret` | Secret du webhook | oui | oui |  | Secret du fournisseur « generic-hmac » du notification-controller (en-tête X-Signature) |
| `target_connector_id` | Connecteur des ressources | non | non |  | Connecteur Kubernetes ou cloud auquel rattacher les événements |



<a id="alertmanager"></a>

## Prometheus Alertmanager

Ajouter un receiver `webhook_configs` vers l'URL Kairn avec `http_config.authorization` (jeton `Bearer` = secret du connecteur).

| | |
|---|---|
| Type | `alertmanager` |
| Nom | Prometheus Alertmanager (incidents) |
| Catégorie | events |
| Fournisseur | `alertmanager` |
| Fréquence de synchronisation | — (réception en push) |
| Ressources | — |
| Métriques d'utilisation | non |
| Facturation réelle | non |
| Webhook entrant | oui |

### Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| webhook sortant | Aucun accès à la source : elle appelle l'URL fournie par Kairn. | oui |

### Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `webhook_secret` | Secret du webhook | oui | oui |  | Jeton transmis par http_config.authorization du receiver (Authorization: Bearer) |
| `target_connector_id` | Connecteur des ressources | non | non |  | Connecteur Kubernetes ou cloud auquel rattacher les événements |



<a id="pagerduty"></a>

## PagerDuty

*Integrations → Generic Webhooks (v3)* : URL Kairn ; le secret de l'abonnement signe les requêtes (`X-PagerDuty-Signature`).

| | |
|---|---|
| Type | `pagerduty` |
| Nom | PagerDuty (incidents) |
| Catégorie | events |
| Fournisseur | `pagerduty` |
| Fréquence de synchronisation | — (réception en push) |
| Ressources | — |
| Métriques d'utilisation | non |
| Facturation réelle | non |
| Webhook entrant | oui |

### Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| webhook sortant | Aucun accès à la source : elle appelle l'URL fournie par Kairn. | oui |

### Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `webhook_secret` | Secret du webhook | oui | oui |  | Secret de l'abonnement webhook v3 (signature X-PagerDuty-Signature) |
| `target_connector_id` | Connecteur des ressources | non | non |  | Connecteur Kubernetes ou cloud auquel rattacher les événements |



<a id="opsgenie"></a>

## Opsgenie

Intégration **Webhook** : URL Kairn et en-tête `Authorization: Bearer <secret du connecteur>`.

| | |
|---|---|
| Type | `opsgenie` |
| Nom | Opsgenie (alertes) |
| Catégorie | events |
| Fournisseur | `opsgenie` |
| Fréquence de synchronisation | — (réception en push) |
| Ressources | — |
| Métriques d'utilisation | non |
| Facturation réelle | non |
| Webhook entrant | oui |

### Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| webhook sortant | Aucun accès à la source : elle appelle l'URL fournie par Kairn. | oui |

### Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `webhook_secret` | Secret du webhook | oui | oui |  | Jeton ajouté en en-tête Authorization: Bearer dans l'intégration webhook Opsgenie |
| `target_connector_id` | Connecteur des ressources | non | non |  | Connecteur Kubernetes ou cloud auquel rattacher les événements |
