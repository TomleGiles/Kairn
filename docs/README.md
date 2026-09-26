# Documentation Kairn

Kairn est une plateforme d'observabilité orientée coûts pour les clouds souverains (OVHcloud, Scaleway, OUTSCALE), OpenStack et Kubernetes. Elle répond à trois questions pour chaque service, projet, équipe ou client : **combien ça coûte**, **est-ce bien utilisé**, **pourquoi ça a bougé**.

## Guides

| Guide | Pour qui |
|---|---|
| [Démarrage](guide/demarrage.md) | Essayer Kairn en 5 minutes, connecter un premier cloud |
| [Déploiement](guide/deploiement.md) | docker compose, Helm (distribué ou mono-nœud), Argo CD, OpenTofu |
| [Configuration](guide/configuration.md) | Référence des variables d'environnement |
| [Modèle de coût](guide/modele-de-cout.md) | Comment chaque montant est calculé et tracé |
| [Sécurité](guide/securite.md) | Isolation, secrets, RBAC, SSO, audit |
| [API, CLI et MCP](guide/api.md) | Intégrations : REST `/api/v1`, CLI `kairn`, Terraform, serveur MCP |

## Références

- [Connecteurs](connectors/README.md) — ressources couvertes, permissions minimales, configuration (générées depuis le code).
- [Décisions d'architecture (ADR)](adr/README.md).
- [OpenAPI](api/openapi.yaml) — spécification générée ; documentation interactive servie par l'API sur `/api/v1/docs`.
- [CHANGELOG](../CHANGELOG.md).
