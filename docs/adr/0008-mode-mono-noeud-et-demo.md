# 0008 — Mode mono-nœud et mode démo

- Statut : acceptée
- Date : 2026-09-26
- Modules : M-13, édition self-hosted

## Contexte

L'architecture cible répartit le travail entre plusieurs services reliés par NATS. Pour l'évaluation, les petites installations self-hosted et le développement, exiger NATS et cinq déploiements est un frein ; l'onboarding vise « premier cloud connecté en moins de 10 minutes ».

## Décision

- **Mode mono-nœud** : si `KAIRN_NATS_URL` n'est pas défini, `kairn-api` utilise un bus en mémoire et exécute **dans son processus** les workers (ingestion, cost-engine, notifier), l'ordonnanceur et la passerelle d'ingestion. PostgreSQL et ClickHouse restent requis ; le comportement fonctionnel est identique (mêmes handlers `pkg/jobs`). Un avertissement est journalisé au démarrage. Le chart Helm le propose (`ci/single-node-values.yaml`).
- **Mode démo** (`KAIRN_MODE=demo`, `make demo`) : tout en mémoire (`memstore`, `memtsdb`), connecteurs simulés OpenStack + Kubernetes, connexion de développement, aucun accès sortant (import des grilles désactivé). Jamais utilisé en production : secrets de démo fixes.
- **Mode distribué** : avec NATS, chaque service s'abonne à ses sujets (groupes de consommateurs JetStream) et l'ordonnanceur tourne en une seule instance (`kairn-ingest scheduler`).

## Conséquences

- Un seul binaire suffit à une petite installation ; la montée en charge consiste à définir `KAIRN_NATS_URL` et déployer les workers.
- En mode mono-nœud, un redémarrage perd les messages en vol du bus mémoire ; l'ordonnanceur les republie à l'échéance suivante (tâches idempotentes, ADR-0005).
- Les tests d'intégration exercent les mêmes handlers avec le bus mémoire.
