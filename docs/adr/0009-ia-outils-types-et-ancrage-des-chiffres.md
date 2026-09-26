# 0009 — IA : outils typés sur l'API et ancrage des chiffres

- Statut : acceptée
- Date : 2026-09-26
- Modules : M-07, M-10, M-12

## Contexte

L'assistant, les explications d'anomalies et le rapport mensuel manipulent des montants destinés à des décideurs. CLAUDE.md impose que l'IA n'accède aux données que via des outils internes typés, que **tous les chiffres viennent des outils** et que chaque réponse liste ses sources. Il prévoyait des « requêtes SQL paramétrées sur un schéma exposé ». Le serveur MCP expose ces outils aux agents des clients « avec les mêmes permissions que l'utilisateur ».

## Décision

- **Les outils appellent l'API publique Kairn avec le jeton de l'utilisateur** (session ou jeton d'API), et non la base : aucun SQL, même paramétré. L'IA hérite ainsi exactement du RBAC, des scopes d'allocation, des limites de plan et de l'isolation (ADR-0002) de la personne qui pose la question. Le jeton de service n'est utilisé que pour les traitements planifiés (rapports) et la comptabilisation de la consommation LLM.
- Outils (`services/ai/kairn_ai/tools.py`) : `get_cost_summary`, `get_costs`, `get_usage`, `get_efficiency`, `list_recommendations`, `get_anomalies`, `get_events`, `get_budget_status`, `get_forecast`, `search_resources`, `list_allocation_nodes`, `render_chart` ; schémas Pydantic stricts (`extra="forbid"`) validés **avant** exécution ; résultats compacts avec identifiant de source citable.
- **Ancrage** (`grounding.py`) : après génération, chaque nombre de la réponse est comparé aux valeurs renvoyées par les outils (et à la question) ; un nombre non ancré est signalé à l'utilisateur et fait échouer le jeu d'évaluation en CI (`services/ai/evals`). Les graphiques ne peuvent tracer que des valeurs issues des outils.
- **Fournisseurs** : Anthropic (défaut SaaS, `claude-opus-5`, raisonnement adaptatif, flux, cache de prompt), Mistral (option souveraine UE) ou modèle local compatible OpenAI (self-hosted). Un appel hors UE (Anthropic) exige l'accord explicite de l'organisation ; `none` désactive l'IA. Le **repli côté serveur** d'Anthropic (`fallbacks: "default"`) est activé : si le modèle principal décline une requête, un autre modèle peut la servir ; la réponse l'indique.
- La consommation (tokens, coût en `Decimal`) est comptabilisée par organisation et soumise au quota mensuel de tokens du plan.

## Conséquences

- Un outil ne peut jamais voir plus que l'utilisateur ; l'agent MCP d'un client non plus. Pas de surface d'injection SQL.
- Latence supplémentaire d'un aller-retour HTTP par outil (négligeable devant l'inférence).
- Un appel d'outil tronqué (`max_tokens`) ou refusé n'est jamais exécuté ; une entrée d'outil invalide est rejetée avant exécution.
- CLAUDE.md §4/§10 est mis à jour : « outils typés sur l'API publique » remplace « SQL paramétré ».

## Alternatives écartées

- *Requêtes SQL paramétrées sur un schéma exposé* : impose de dupliquer RBAC, scopes et limites de plan dans une couche SQL ; une erreur y serait une fuite multi-tenant.
