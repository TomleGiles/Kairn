"""Prévision de coût de fin de mois (M-09) : Holt-Winters hebdomadaire amorti."""

from __future__ import annotations

import math
from datetime import datetime, timedelta
from decimal import Decimal

from .models import Forecast, ForecastPoint
from .money import cents, dec
from .stats import holt_winters

Z95 = 1.96


def month_end(day: datetime) -> datetime:
    if day.month == 12:
        return day.replace(year=day.year + 1, month=1, day=1, hour=0, minute=0, second=0, microsecond=0)
    return day.replace(month=day.month + 1, day=1, hour=0, minute=0, second=0, microsecond=0)


def forecast_month(
    days: list[datetime], values: list[Decimal], today: datetime, currency: str, node_id: str = "",
) -> Forecast:
    """Prévoit le total du mois courant.

    `days`/`values` contiennent l'historique journalier des jours complets
    (antérieurs à `today`). Total = réel des jours complets du mois + prévision
    de `today` jusqu'à la fin du mois, avec intervalle de confiance à 95 %.
    """
    today = today.replace(hour=0, minute=0, second=0, microsecond=0)
    end = month_end(today)
    start_month = today.replace(day=1)
    horizon = (end - today).days
    hist = [float(v) for v in values]
    hw = holt_winters(hist, horizon)
    actual = sum((v for d, v in zip(days, values, strict=False) if d >= start_month and d < today), Decimal(0))
    points: list[ForecastPoint] = []
    total = actual
    var = 0.0
    for h, v in enumerate(hw.forecast, start=1):
        v = max(v, 0.0)
        spread = Z95 * hw.sigma * math.sqrt(h)
        day = today + timedelta(days=h - 1)
        points.append(ForecastPoint(day=day, value=cents(dec(v)), lower=cents(dec(max(v - spread, 0.0))),
                                    upper=cents(dec(v + spread))))
        total += dec(v)
        var += (hw.sigma**2) * h
    half = dec(Z95 * math.sqrt(var)) if horizon > 0 else Decimal(0)
    return Forecast(
        node_id=node_id, generated_at=datetime.now(tz=today.tzinfo), model="holt-winters-weekly-damped",
        currency=currency, points=points, period_end=end, total=cents(total),
        lower=cents(max(total - half, actual)), upper=cents(total + half),
    )
