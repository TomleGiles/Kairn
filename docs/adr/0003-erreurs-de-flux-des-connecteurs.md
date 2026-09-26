# 0003 — Erreurs en cours de flux des connecteurs

- Statut : acceptée
- Date : 2026-09-26
- Modules : M-01, M-02

## Contexte

Le contrat `Connector` (CLAUDE.md §6) renvoie des canaux (`<-chan Resource`, `<-chan MetricPoint`, `<-chan CostLine`) pour traiter de gros volumes en flux. Un canal ne peut pas porter d'erreur survenant *après* le début du flux (page 12 d'une pagination en échec, jeton expiré…). Or l'inventaire est historisé : une ressource absente d'une synchronisation *complète* est marquée supprimée (`valid_to`). Confondre une synchronisation partielle avec une synchronisation complète supprimerait à tort des ressources, puis fausserait les coûts.

## Décision

Le contrat reste inchangé ; les erreurs de flux passent par un **collecteur attaché au contexte** (`pkg/connector/stream.go`) :

- l'appelant crée `ctx, sink := connector.WithErrorSink(ctx)` ;
- le connecteur appelle `connector.ReportError(ctx, err)` puis ferme son canal ;
- après avoir drainé le canal, l'appelant consulte `sink.Err()` : en cas d'erreur, l'instantané d'inventaire est marqué **incomplet** (`Complete: false`) — les ressources reçues sont enregistrées, mais **aucune suppression** n'est déduite — et la synchronisation est remontée en échec (statut du connecteur, alerte « connecteur en échec »).

Les méthodes facultatives renvoient `connector.ErrNotSupported` (ex. `SyncBilling`), traité comme « rien à faire ».

## Conséquences

- Aucune suppression fantôme lors d'une panne partielle d'API fournisseur.
- Les tests de conformité (`connectors/all/conformance_test.go`) vérifient pour chaque connecteur la déclaration (permissions, champs secrets), `ErrNotSupported` pour les méthodes non couvertes et un échec propre face à une API qui refuse tout ; les tests par fixtures de chaque connecteur vérifient le collecteur d'erreurs.
- Un connecteur qui oublierait `ReportError` et fermerait son canal prématurément produirait une synchronisation vue comme complète : la revue de code et les tests par fixtures (réponses API en erreur) doivent couvrir ce cas.

## Alternatives écartées

- *Canal de résultats `Result{Value, Err}`* : casse le contrat du §6 et alourdit tous les consommateurs.
- *Collecter tout en mémoire puis renvoyer `([]Resource, error)`* : incompatible avec les volumes visés (backfill de 13 mois, 10 000 ressources).
