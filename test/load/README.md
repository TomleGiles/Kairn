# Tests de charge (k6)

Vérifient les exigences de performance de CLAUDE.md §3 avant chaque release :

| Scénario | Exigence | Seuil k6 |
|---|---|---|
| [`dashboards.js`](dashboards.js) | Tableaux de bord < 1,5 s (P95) sur 12 mois de données, organisation de 10 000 ressources | `http_req_duration{kind:dashboard\|explorer}` p(95) < 1500 ms, erreurs < 1 % |
| [`ingestion.js`](ingestion.js) | 1 M points/min par worker | `points_accepted` > 16 667/s, erreurs < 0,1 % |

```bash
make load                                   # les deux scénarios contre la pile locale
k6 run test/load/dashboards.js              # instance de démo (connexion demo@kairn.local)
KAIRN_URL=https://staging.kairn.example KAIRN_TOKEN=kairn_… KAIRN_ORG=<id> VUS=50 k6 run test/load/dashboards.js
KAIRN_GATEWAY=http://localhost:8081 AGENT_TOKEN=<jeton> k6 run test/load/ingestion.js
```

- Le jeton `AGENT_TOKEN` est celui d'un connecteur « Agent Kairn » (fin de son URL de réception).
- Pour être représentatif, `dashboards.js` doit viser une organisation chargée avec 12 mois d'historique et ~10 000 ressources (environnement de pré-production alimenté par backfill), sur la pile distribuée (ClickHouse, NATS) — pas sur l'instance de démo en mémoire.
- Les seuils font échouer k6 (code de sortie non nul) : le résultat conditionne la release.
