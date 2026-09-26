# Webhooks de déploiement et d'incident

Ces sources alimentent la **corrélation des anomalies** (M-07) : quand un coût ou une utilisation dérive, Kairn recherche dans la fenêtre les déploiements, synchronisations, réconciliations et incidents reçus. Elles n'ont besoin d'**aucun accès** à l'outil source : c'est lui qui appelle l'URL fournie par Kairn.

Pour chaque source, créer le connecteur dans Kairn : l'URL de réception (`https://<kairn>/ingest/v1/webhooks/<jeton>`) figure dans la fiche du connecteur. Elle contient un jeton secret : elle n'est visible que des membres autorisés à lire les connecteurs et peut être régénérée (rotation) à tout moment. Chaque requête est authentifiée par le secret configuré (signature HMAC ou jeton selon la source) ; une requête non authentifiée est rejetée. `target_connector_id` rattache les événements aux ressources d'un connecteur Kubernetes ou cloud (par nom de service / namespace).

<a id="gitlab"></a>

## GitLab

*Settings → Webhooks* du projet : URL Kairn, **Secret token** = secret du connecteur (en-tête `X-Gitlab-Token`), cocher **Deployment events**.

<!-- type:gitlab -->

<a id="github"></a>

## GitHub

*Settings → Webhooks* : URL Kairn, *Content type* `application/json`, **Secret** = secret du connecteur (signature `X-Hub-Signature-256`), événement **Deployment statuses**.

<!-- type:github -->

<a id="argo-cd"></a>

## Argo CD

Dans `argocd-notifications-cm`, déclarer un service webhook vers l'URL Kairn avec l'en-tête `X-Webhook-Secret` et un déclencheur sur `on-sync-succeeded` / `on-sync-failed`.

<!-- type:argocd -->

<a id="flux"></a>

## Flux

Créer un `Provider` du notification-controller de type `generic-hmac` pointant vers l'URL Kairn, avec le secret du connecteur (en-tête `X-Signature`), et une `Alert` sur les `Kustomization` / `HelmRelease` suivies.

<!-- type:flux -->

<a id="alertmanager"></a>

## Prometheus Alertmanager

Ajouter un receiver `webhook_configs` vers l'URL Kairn avec `http_config.authorization` (jeton `Bearer` = secret du connecteur).

<!-- type:alertmanager -->

<a id="pagerduty"></a>

## PagerDuty

*Integrations → Generic Webhooks (v3)* : URL Kairn ; le secret de l'abonnement signe les requêtes (`X-PagerDuty-Signature`).

<!-- type:pagerduty -->

<a id="opsgenie"></a>

## Opsgenie

Intégration **Webhook** : URL Kairn et en-tête `Authorization: Bearer <secret du connecteur>`.

<!-- type:opsgenie -->
