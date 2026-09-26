"""Kairn analytics : anomalies, corrélation, prévisions et recommandations.

Le service ne lit et n'écrit les données que via l'API interne Kairn
(`/internal/v1`), authentifié par jeton de service. Les montants sont des
`Decimal` ; les flottants ne servent qu'aux calculs statistiques et sont
quantifiés avant toute publication.
"""

__version__ = "0.1.0"
