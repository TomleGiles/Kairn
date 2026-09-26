"""Contrôle de « grounding » : chaque chiffre significatif d'une réponse doit
figurer dans les résultats d'outils (ou dans la question de l'utilisateur).

Les montants sont comparés après normalisation (séparateurs français et
anglais, espaces fines, arrondi au centime et à l'unité), les pourcentages
après arrondi à 0 ou 1 décimale. Sont ignorés : dates, années, heures et
petits entiers (≤ 31), qui relèvent de la formulation plutôt que des données.
"""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from decimal import ROUND_HALF_UP, Decimal, InvalidOperation
from typing import Any

# Nombre avec séparateurs de milliers (espace, espace fine, point, virgule) et décimales.
_NUM = re.compile(r"(?<![\w/.:-])[-−]?\d{1,3}(?:[   .,]\d{3})*(?:[.,]\d+)?(?![\w/:])|(?<![\w/.:-])[-−]?\d+(?:[.,]\d+)?(?![\w/:])")
# Dates (13/09, 13/09/2026, 2026-09-13) et heures (10:12, 10h12) ; le point est réservé aux décimales.
_DATE = re.compile(r"\b\d{1,2}/\d{1,2}(?:/\d{2,4})?\b|\b\d{4}-\d{2}(?:-\d{2})?\b|\b\d{1,2}[:h]\d{2}\b")


def _candidates(token: str) -> set[Decimal]:
    """Interprétations possibles d'un nombre écrit (formats français et anglais)."""
    t = token.replace("−", "-").replace(" ", " ").replace(" ", " ").strip()
    neg = t.startswith("-")
    t = t.lstrip("-").replace(" ", "")
    seps = [c for c in t if c in ".,"]
    readings: list[str] = []
    if not seps:
        readings.append(t)
    elif len(set(seps)) == 2:
        dec_sep = t[max(t.rfind("."), t.rfind(","))]
        other = "," if dec_sep == "." else "."
        readings.append(t.replace(other, "").replace(dec_sep, "."))
    elif len(seps) > 1:
        readings.append(t.replace(seps[0], ""))  # séparateurs de milliers répétés
    else:
        sep = seps[0]
        head, tail = t.split(sep)
        readings.append(head + "." + tail)  # séparateur décimal
        if len(tail) == 3:
            readings.append(head + tail)  # ou séparateur de milliers
    out: set[Decimal] = set()
    for r in readings:
        try:
            v = Decimal(r)
        except InvalidOperation:
            continue
        out.add(-v if neg else v)
    return out


def numbers_in_text(text: str) -> list[tuple[str, set[Decimal]]]:
    """Extrait les nombres significatifs d'un texte (hors dates, heures, années, petits entiers)."""
    cleaned = _DATE.sub(" ", text)
    out: list[tuple[str, set[Decimal]]] = []
    for m in _NUM.finditer(cleaned):
        tok = m.group(0).strip()
        cands = _candidates(tok)
        if not cands:
            continue
        if all(v == v.to_integral_value() and (abs(v) <= 31 or 1990 <= v <= 2100) for v in cands):
            continue
        out.append((tok, cands))
    return out


def _collect(value: Any, acc: set[Decimal]) -> None:
    if isinstance(value, dict):
        for v in value.values():
            _collect(v, acc)
    elif isinstance(value, list):
        for v in value:
            _collect(v, acc)
    elif isinstance(value, bool):
        return
    elif isinstance(value, int | float):
        try:
            acc.add(Decimal(repr(value)))
        except InvalidOperation:
            return
    elif isinstance(value, str):
        s = value.strip()
        if re.fullmatch(r"-?\d+(\.\d+)?", s):
            acc.add(Decimal(s))
        else:
            for _, cands in numbers_in_text(s):
                acc |= cands


def value_variants(v: Decimal) -> set[Decimal]:
    """Formes arrondies acceptables d'une valeur de référence."""
    out = {v}
    for q in ("0.01", "0.1", "1"):
        out.add(v.quantize(Decimal(q), rounding=ROUND_HALF_UP))
    # Ratios 0..1 exprimés en pourcentage.
    if Decimal(-1) <= v <= Decimal(1):
        p = v * 100
        for q in ("0.1", "1"):
            out.add(p.quantize(Decimal(q), rounding=ROUND_HALF_UP))
    # Milliers (« 12,3 k€ »).
    if abs(v) >= 1000:
        out.add((v / 1000).quantize(Decimal("0.1"), rounding=ROUND_HALF_UP))
    return {x.normalize() for x in out}


@dataclass
class GroundingReport:
    checked: int
    ungrounded: list[str]

    @property
    def ok(self) -> bool:
        return not self.ungrounded


def reference_values(sources: list[Any]) -> set[Decimal]:
    acc: set[Decimal] = set()
    for s in sources:
        if isinstance(s, str):
            try:
                _collect(json.loads(s), acc)
            except ValueError:
                _collect(s, acc)
        else:
            _collect(s, acc)
    ref: set[Decimal] = set()
    for v in acc:
        ref |= value_variants(v)
    return ref


def check(answer: str, sources: list[Any], question: str = "") -> GroundingReport:
    """Vérifie que les nombres de `answer` proviennent de `sources` ou de la question."""
    ref = reference_values([*sources, question])
    missing: list[str] = []
    found = numbers_in_text(answer)
    for tok, cands in found:
        norm = {c.normalize() for c in cands} | {abs(c).normalize() for c in cands}
        if not norm & ref:
            missing.append(tok)
    return GroundingReport(checked=len(found), ungrounded=missing)
