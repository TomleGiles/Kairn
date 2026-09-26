# Agent Kairn

> Page générée par `make gen` à partir des déclarations des connecteurs. Pour la modifier, éditer `services/api/cmd/docs/notes/agent.md` ou le connecteur.

| | |
|---|---|
| Type | `agent` |
| Nom | Agent Kairn (hôtes on-prem / VM) |
| Catégorie | agent |
| Fournisseur | `onprem` |
| Fréquence de synchronisation | 5 min |
| Ressources | `host` |
| Métriques d'utilisation | oui |
| Facturation réelle | non |
| Webhook entrant | oui |

## Permissions minimales

| Portée | Usage | Obligatoire |
|---|---|---|
| sortant HTTPS vers la passerelle | L'agent pousse ses données ; aucun accès entrant n'est requis | oui |
| lecture /proc, /sys | Collecte CPU, mémoire, disque et réseau de l'hôte (utilisateur non privilégié) | oui |

## Configuration

| Champ | Libellé | Obligatoire | Secret | Défaut | Aide |
|---|---|---|---|---|---|
| `site` | Site / datacenter | non | non |  | Libellé ajouté aux hôtes (label site) |

Pour les hôtes et VM **sans pile de métriques** (serveurs on-prem, VM d'un cloud privé sans Gnocchi). Binaire Go unique, sans privilège : il lit `/proc` et `/sys`, pousse ses métriques en OTLP/HTTP (protobuf) vers la passerelle d'ingestion et déclare l'hôte dans l'inventaire. Aucun port entrant n'est ouvert sur l'hôte.

## Installation

1. Créer un connecteur « Agent Kairn » : son URL de réception se termine par le **jeton du connecteur** (secret, régénérable), à fournir à l'agent.
2. Déployer l'agent :

- **Kubernetes** (DaemonSet, un agent par node) : [`deploy/kubernetes/kairn-agent.yaml`](../../deploy/kubernetes/kairn-agent.yaml) — monter le jeton dans un secret et renseigner `KAIRN_AGENT_GATEWAY`.
- **systemd** : [`deploy/kubernetes/kairn-agent.service`](../../deploy/kubernetes/kairn-agent.service).

```bash
KAIRN_AGENT_GATEWAY=https://kairn.example.com \
KAIRN_AGENT_TOKEN_FILE=/etc/kairn/agent-token \
kairn-agent --label site=datacenter-1
```

Options : `--interval` (défaut 30 s, minimum 10 s), `--label clé=valeur` (répétable, devient un label d'allocation), `--host-root /host` (conteneur), `--host-id` (défaut `/etc/machine-id`), `--once`.

## Comportement

- La passerelle doit être en HTTPS (le jeton est un secret) ; seul `http://localhost` est accepté en clair pour les tests.
- En cas de coupure réseau, les points sont conservés en mémoire (20 000 au plus, les plus anciens sont abandonnés) et renvoyés au rétablissement.
- L'inventaire de l'hôte (vCPU, RAM, disque, OS) est rafraîchi toutes les heures ; le coût de l'hôte vient du modèle de coût on-prem (matériel amorti, énergie, licences, main-d'œuvre) appliqué par sélecteur de labels ou par connecteur.
