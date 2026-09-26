"""Kairn IA : assistant conversationnel, rapports mensuels, explications et serveur MCP.

Principe cardinal (CLAUDE.md §M-10) : l'IA n'accède aux données que via des
outils typés qui appellent l'API Kairn avec les droits de l'utilisateur, et
tout chiffre cité doit provenir d'un résultat d'outil. Un contrôle de
« grounding » vérifie chaque réponse avant de la marquer comme fiable.
"""

__version__ = "0.1.0"
