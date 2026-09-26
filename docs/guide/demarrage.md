# Démarrage

## Essayer sans rien installer d'autre que Go et Node.js

Le mode démo tient en un processus, tout en mémoire, avec une organisation d'exemple (cloud OpenStack OVHcloud simulé + cluster Kubernetes) et 60 jours d'historique :

```bash
make demo
```

- Interface : <http://localhost:3000> (connexion de développement, sans mot de passe) ;
- API : <http://localhost:8080/api/v1/docs>.

Sous Windows sans `make` : `scripts/make.ps1 demo`.

Le mode démo n'accède à aucun service externe (les grilles tarifaires sont des grilles d'exemple indicatives) et ne doit jamais être exposé.

## Pile locale complète

Prérequis : Docker (ou Podman) avec Compose.

```bash
make env     # génère .env avec des secrets aléatoires (une seule fois)
make dev     # PostgreSQL, ClickHouse, NATS, Valkey, MinIO, Keycloak, Mailpit + services Kairn
make seed    # organisation de démonstration (optionnel)
```

| Service | URL locale |
|---|---|
| Interface web | <http://localhost:3000> |
| API + documentation OpenAPI | <http://localhost:8080/api/v1/docs> |
| Passerelle d'ingestion (webhooks, OTLP) | <http://localhost:8081> |
| Keycloak (SSO) | <http://localhost:8180> |
| Mailpit (e-mails de test) | <http://localhost:8025> |

## Connecter un premier cloud (objectif : moins de 10 minutes)

1. *Connecteurs → Ajouter* : choisir le fournisseur (OpenStack, OVHcloud, Scaleway, OUTSCALE, Kubernetes…).
2. Créer un accès **en lecture seule** en suivant la page du connecteur ([liste](../connectors/README.md)) ; la liste des permissions minimales est affichée dans l'assistant.
3. Kairn valide l'accès puis lance la première synchronisation et un **backfill** de l'historique disponible (jusqu'à 13 mois selon la source).
4. Les premiers coûts apparaissent dès la fin du premier calcul (quelques minutes ; objectif < 1 h). Les grilles publiques OVHcloud, Scaleway et OUTSCALE sont importées automatiquement chaque jour.

Ensuite :

- **Allocation** : définir la hiérarchie (business unit → équipe → service → environnement) et les règles par labels/tags, projets ou namespaces ; viser plus de 95 % de coûts attribués.
- **Budgets et alertes** : seuils réels et prévisionnels, canaux e-mail, Slack, Teams, Mattermost, webhook, PagerDuty.
- **Usage** : ajouter Prometheus, Gnocchi ou l'agent Kairn pour les vues coût × utilisation et les recommandations de dimensionnement.
- **Assistant IA** : activer le fournisseur LLM autorisé par l'organisation (Anthropic, Mistral ou modèle local) dans les paramètres.

## Commandes utiles

```bash
make test       # tests unitaires Go + Python
make test-int   # intégration PostgreSQL (RLS) et ClickHouse sur la pile docker compose
make lint       # golangci-lint, gofmt, ruff, mypy, eslint, tsc
make gen        # OpenAPI, documentation des connecteurs, client TypeScript
make e2e        # Playwright (instance de démo)
make load       # scénarios k6
```
