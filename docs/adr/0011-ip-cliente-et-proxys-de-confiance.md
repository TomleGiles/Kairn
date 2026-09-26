# 0011 — IP cliente et proxys de confiance

- Statut : acceptée
- Date : 2026-09-26
- Modules : M-11, sécurité

## Contexte

L'IP cliente sert à la limitation de débit des requêtes anonymes (connexion, webhooks) et au journal d'audit. Derrière un ingress, l'IP de connexion est celle du proxy ; l'en-tête `X-Forwarded-For` donne l'IP réelle, mais **n'importe quel client peut le forger**. Accepter cet en-tête inconditionnellement (middleware `RealIP` de chi) permettait de contourner la limitation de débit et de falsifier l'audit.

## Décision

- `X-Forwarded-For` n'est lu **que si** la connexion provient d'un proxy de confiance, déclaré par `KAIRN_TRUSTED_PROXIES` (CIDR ou IP ; Helm : `config.trustedProxies`, par défaut les réseaux privés du cluster).
- L'en-tête est parcouru **de droite à gauche** : la première adresse qui n'est pas un proxy de confiance est l'IP cliente (les entrées de gauche, fournies par le client, sont ignorées).
- Le port est retiré de l'adresse (une clé de limitation par IP, et non par connexion).
- Sans configuration, seule l'adresse de connexion est utilisée.

## Conséquences

- Pas de contournement de la limitation de débit par en-tête forgé ; audit fiable.
- Une configuration trop large (`0.0.0.0/0`) réintroduirait la faille : la documentation et les valeurs Helm l'interdisent explicitement.
