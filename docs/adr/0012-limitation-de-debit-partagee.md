# 0012 — Limitation de débit partagée (Redis/Valkey)

- Statut : acceptée
- Date : 2026-09-26
- Modules : M-12, M-13, sécurité

## Contexte

L'API limite le débit par principal (utilisateur, jeton d'API) ou par IP pour les requêtes anonymes (20 requêtes/s, rafale de 60). Un seau à jetons en mémoire est propre à chaque réplica : avec N réplicas derrière un répartiteur, la limite effective devient N fois la limite annoncée, et un redémarrage la réinitialise. CLAUDE.md prévoit Redis/Valkey pour le rate limiting.

## Décision

- `pkg/ratelimit` définit `Limiter` avec deux implémentations : `Memory` (seau local) et `Redis` (seau partagé).
- Le seau Redis est appliqué **atomiquement** par un script Lua (lecture, recharge, décrément, expiration) sur la clé `kairn:rl:<principal>` ; l'horodatage vient de l'API. Les clés inactives expirent d'elles-mêmes.
- Si `KAIRN_REDIS_URL` est défini, l'API utilise le seau partagé ; sinon le seau local (mode démo, mono-nœud à un réplica).
- **Dégradation** : si Redis ne répond pas (délai de 200 ms), la requête est évaluée par un seau local de repli — l'API ne s'arrête pas et ne devient pas illimitée. L'incident est journalisé (au plus une fois par minute) et le retour à la normale aussi.
- Nouvelle dépendance : `github.com/redis/go-redis/v9` (et `alicebob/miniredis` pour les tests).

## Conséquences

- Limite cohérente quel que soit le nombre de réplicas.
- Un aller-retour Redis par requête authentifiée (sub-milliseconde dans le cluster).
- Les tests vérifient le partage entre deux réplicas, la recharge, l'expiration et le repli.

## Alternatives écartées

- *Limitation à l'ingress seulement* : ne connaît pas le principal authentifié (jeton d'API partagé par plusieurs IP).
- *Fenêtre fixe (`INCR` + `EXPIRE`)* : plus simple mais autorise des rafales doubles à la frontière des fenêtres.
