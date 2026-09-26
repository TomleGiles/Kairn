"""Conversions monétaires : les montants ne transitent jamais par un float publié."""

from __future__ import annotations

from decimal import ROUND_HALF_UP, Decimal

HOURS_PER_MONTH = Decimal(730)
CENT = Decimal("0.01")
MICRO = Decimal("0.000001")


def dec(x: float | int | str | Decimal) -> Decimal:
    """Convertit une valeur statistique en Decimal via sa représentation courte."""
    if isinstance(x, Decimal):
        return x
    if isinstance(x, float):
        return Decimal(repr(round(x, 9)))
    return Decimal(str(x))


def cents(x: Decimal) -> Decimal:
    """Arrondit au centime (arrondi commercial)."""
    return x.quantize(CENT, rounding=ROUND_HALF_UP)


def micros(x: Decimal) -> Decimal:
    """Arrondit à la précision de stockage (6 décimales)."""
    return x.quantize(MICRO, rounding=ROUND_HALF_UP)


def fmt_eur(x: Decimal, currency: str = "EUR", locale: str = "fr") -> str:
    """Formate un montant pour un texte explicatif (ex. « 1 234,56 € »)."""
    q = cents(x)
    sign = "-" if q < 0 else ""
    q = abs(q)
    whole, frac = f"{q:.2f}".split(".")
    groups: list[str] = []
    while whole:
        groups.insert(0, whole[-3:])
        whole = whole[:-3]
    if locale == "fr":
        sym = {"EUR": " €", "USD": " $", "GBP": " £", "CHF": " CHF"}.get(currency, " " + currency)
        thin = " "
        return f"{sign}{thin.join(groups)},{frac}{sym}"
    sym = {"EUR": "€", "USD": "$", "GBP": "£"}.get(currency, currency + " ")
    return f"{sign}{sym}{','.join(groups)}.{frac}"
