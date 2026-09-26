"""Rapport mensuel exécutif (M-10) : faits calculés par Kairn, synthèse
rédigée par l'IA (vérifiée chiffre par chiffre) ou par gabarit, PDF déposé
dans Kairn puis envoyé par e-mail par le worker.

Les faits proviennent exclusivement de l'API Kairn ; l'IA ne fait que
formuler. Si la synthèse contient un chiffre absent des faits, elle est
écartée au profit du gabarit déterministe.
"""

from __future__ import annotations

import base64
import json
import logging
import os
import re
from dataclasses import dataclass, field
from datetime import UTC, date, datetime
from decimal import ROUND_HALF_UP, Decimal
from pathlib import Path
from typing import Any

from fpdf import FPDF
from fpdf.enums import XPos, YPos
from pydantic import BaseModel, Field, ValidationError

from . import grounding, prompts
from .assistant import usage_payload
from .config import Settings
from .kairn import KairnError, ServiceAPI
from .providers import Provider, ProviderUnavailable, for_org

log = logging.getLogger(__name__)

CENT = Decimal("0.01")


# --------------------------------------------------------------------------- faits


def _d(v: Any) -> Decimal:
    try:
        return Decimal(str(v))
    except ArithmeticError:
        return Decimal(0)


def _month_bounds(period: str) -> tuple[date, date]:
    y, m = (int(x) for x in period.split("-"))
    start = date(y, m, 1)
    end = date(y + (m == 12), m % 12 + 1, 1)
    return start, end


def _prev(period_start: date) -> date:
    return date(period_start.year - (period_start.month == 1), (period_start.month - 2) % 12 + 1, 1)


def _iso(d: date) -> str:
    return f"{d.isoformat()}T00:00:00Z"


def pct(new: Decimal, old: Decimal) -> Decimal | None:
    if old == 0:
        return None
    return ((new - old) / old * 100).quantize(Decimal("0.1"), rounding=ROUND_HALF_UP)


@dataclass
class Facts:
    org_name: str
    period: str
    currency: str
    partial: bool  # mois en cours (à date)
    days: int
    total: Decimal
    previous_total: Decimal
    change_percent: Decimal | None
    forecast_month_end: Decimal | None
    previous_complete: bool = True
    by_provider: list[dict[str, Any]] = field(default_factory=list)
    by_team: list[dict[str, Any]] = field(default_factory=list)
    variations: list[dict[str, Any]] = field(default_factory=list)
    anomalies: list[dict[str, Any]] = field(default_factory=list)
    savings_realized_monthly: Decimal = Decimal(0)
    savings_accepted_monthly: Decimal = Decimal(0)
    savings_potential_monthly: Decimal = Decimal(0)
    open_recommendations: int = 0
    actions: list[dict[str, Any]] = field(default_factory=list)
    budgets: list[dict[str, Any]] = field(default_factory=list)

    def as_json(self) -> dict[str, Any]:
        def conv(v: Any) -> Any:
            if isinstance(v, Decimal):
                return str(v)
            if isinstance(v, list):
                return [conv(x) for x in v]
            if isinstance(v, dict):
                return {k: conv(x) for k, x in v.items()}
            return v

        return {k: conv(v) for k, v in self.__dict__.items()}


def _rows(api: ServiceAPI, org_id: str, start: date, end: date, group_by: list[str]) -> list[dict[str, Any]]:
    res = api.get(f"/api/v1/orgs/{org_id}/costs", {"from": _iso(start), "to": _iso(end), "granularity": "total", "group_by": group_by})
    return list(res.get("rows") or [])


def _total(api: ServiceAPI, org_id: str, start: date, end: date) -> Decimal:
    res = api.get(f"/api/v1/orgs/{org_id}/costs", {"from": _iso(start), "to": _iso(end), "granularity": "total"})
    return _d(res.get("total", 0)).quantize(CENT)


def collect(api: ServiceAPI, org: dict[str, Any], period: str, today: date | None = None) -> Facts:
    """Calcule les faits du rapport à partir de l'API Kairn (jeton de service)."""
    org_id = str(org["id"])
    currency = str(org.get("currency") or "EUR")
    today = today or datetime.now(tz=UTC).date()
    start, end = _month_bounds(period)
    partial = start <= today < end
    cut = min(end, date.fromordinal(today.toordinal() + 1)) if partial else end
    days = (cut - start).days
    pstart = _prev(start)
    # Mois en cours : comparaison à date égale avec le mois précédent.
    pend = date.fromordinal(pstart.toordinal() + days) if partial else start
    pend = min(pend, start)

    total = _total(api, org_id, start, cut)
    previous = _total(api, org_id, pstart, pend)
    facts = Facts(
        org_name=str(org.get("name") or ""),
        period=period,
        currency=currency,
        partial=partial,
        days=days,
        total=total,
        previous_total=previous,
        change_percent=pct(total, previous),
        forecast_month_end=None,
    )

    if partial:
        try:
            s = api.get(f"/api/v1/orgs/{org_id}/costs/summary")
            facts.forecast_month_end = _d(s.get("forecast_month_end")).quantize(CENT)
        except KairnError:
            pass

    for r in sorted(_rows(api, org_id, start, cut, ["provider"]), key=lambda r: -_d(r["amount"]))[:6]:
        facts.by_provider.append({"name": r["keys"].get("provider") or "—", "amount": _d(r["amount"]).quantize(CENT)})

    names: dict[str, str] = {}
    kinds: dict[str, str] = {}
    parents: dict[str, str] = {}
    try:
        for n in api.get(f"/api/v1/orgs/{org_id}/allocation/nodes", {"limit": 500}).get("items") or []:
            names[n["id"]] = n["name"]
            kinds[n["id"]] = n.get("kind", "")
            parents[n["id"]] = n.get("parent_id") or ""
    except KairnError:
        pass

    def team_of(node: str) -> str:
        """Remonte au nœud « équipe » (les coûts sont souvent alloués à un service ou un environnement)."""
        cur, seen = node, set()
        while cur and cur not in seen:
            if kinds.get(cur) == "team":
                return cur
            seen.add(cur)
            cur = parents.get(cur, "")
        return node

    # Comparaison fiable seulement si les données couvrent toute la période précédente.
    prev_days = api.get(f"/api/v1/orgs/{org_id}/costs", {"from": _iso(pstart), "to": _iso(pend), "granularity": "day"}).get("rows") or []
    first = min((str(r["period"])[:10] for r in prev_days if _d(r["amount"]) != 0), default="")
    facts.previous_complete = bool(first) and first <= pstart.isoformat()
    if not facts.previous_complete:
        facts.change_percent = None

    unallocated = "Non alloué" if org.get("locale", "fr") != "en" else "Unallocated"
    cur: dict[str, Decimal] = {}
    old: dict[str, Decimal] = {}
    for bucket, (since, until) in ((cur, (start, cut)), (old, (pstart, pend))):
        for r in _rows(api, org_id, since, until, ["allocation_node_id"]):
            key = team_of(r["keys"].get("allocation_node_id", ""))
            bucket[key] = bucket.get(key, Decimal(0)) + _d(r["amount"])
    rows: list[dict[str, Any]] = []
    for node in set(cur) | set(old):
        a, b = cur.get(node, Decimal(0)), old.get(node, Decimal(0))
        rows.append(
            {
                "name": names.get(node, unallocated),
                "amount": a.quantize(CENT),
                "previous": b.quantize(CENT),
                "delta": (a - b).quantize(CENT),
                "change_percent": pct(a, b) if facts.previous_complete else None,
            }
        )
    facts.by_team = [r for r in sorted(rows, key=lambda r: -r["amount"]) if r["amount"] != 0 or r["previous"] != 0][:8]
    if facts.previous_complete:
        facts.variations = [r for r in sorted(rows, key=lambda r: -abs(r["delta"])) if r["delta"] != 0][:3]

    try:
        anomalies = api.get(f"/api/v1/orgs/{org_id}/anomalies", {"from": _iso(pstart), "to": _iso(cut)}) or []
    except KairnError:
        anomalies = []
    in_period = [a for a in anomalies if a.get("kind") == "cost" and str(a.get("window_end", ""))[:10] >= start.isoformat()]
    for a in sorted(in_period, key=lambda a: -abs(_d(a["actual"]) - _d(a["expected"])))[:3]:
        facts.anomalies.append(
            {
                "title": a.get("title", ""),
                "expected": _d(a["expected"]).quantize(CENT),
                "actual": _d(a["actual"]).quantize(CENT),
                "explanation": a.get("explanation", ""),
                "day": str(a.get("window_start", ""))[:10],
            }
        )

    try:
        s = api.get(f"/api/v1/orgs/{org_id}/recommendations/summary")
        facts.savings_realized_monthly = _d(s.get("realized_monthly")).quantize(CENT)
        facts.savings_accepted_monthly = _d(s.get("accepted_monthly")).quantize(CENT)
        facts.savings_potential_monthly = _d(s.get("potential_monthly")).quantize(CENT)
        facts.open_recommendations = int(s.get("open_count") or 0)
        recos = api.get(f"/api/v1/orgs/{org_id}/recommendations", {"status": ["open"], "limit": 50}).get("items") or []
        risk_rank = {"low": 0, "medium": 1, "high": 2}
        recos.sort(key=lambda r: (-_d(r["savings_monthly"]), risk_rank.get(r.get("risk", ""), 3)))
        facts.actions = [
            {"title": r["title"], "savings_monthly": _d(r["savings_monthly"]).quantize(CENT), "risk": r.get("risk", "")} for r in recos[:3]
        ]
    except KairnError:
        pass

    if partial:
        try:
            for b in api.get(f"/api/v1/orgs/{org_id}/budgets-status") or []:
                facts.budgets.append(
                    {
                        "name": b["budget"]["name"],
                        "amount": _d(b["budget"]["amount"]).quantize(CENT),
                        "actual": _d(b["actual"]).quantize(CENT),
                        "forecast": _d(b["forecast"]).quantize(CENT),
                        "forecast_percent": _d(b["forecast_percent"]).quantize(Decimal("0.1")),
                    }
                )
        except KairnError:
            pass
    return facts


# --------------------------------------------------------------------------- formulation


def money(v: Decimal, currency: str, locale: str = "fr") -> str:
    q = v.quantize(CENT, rounding=ROUND_HALF_UP)
    sign = "-" if q < 0 else ""
    whole, frac = f"{abs(q):.2f}".split(".")
    groups = re.sub(r"(?<=\d)(?=(\d{3})+$)", "\u202f" if locale == "fr" else ",", whole)
    symbol = {"EUR": "€", "USD": "$", "GBP": "£", "CHF": "CHF"}.get(currency, currency)
    if locale == "fr":
        return f"{sign}{groups},{frac}\u00a0{symbol}"
    return f"{sign}{symbol}{groups}.{frac}" if symbol in "€$£" else f"{sign}{groups}.{frac} {symbol}"


def percent(v: Decimal | None, locale: str = "fr") -> str:
    if v is None:
        return "—"
    s = f"{v:+.1f}"
    return (s.replace(".", ",") + "\u00a0%") if locale == "fr" else s + "%"


class Narrative(BaseModel):
    headline: str = Field(max_length=300)
    summary: str = Field(max_length=2000)
    variations: list[str] = Field(default_factory=list, max_length=6)
    actions: list[str] = Field(default_factory=list, max_length=3)


def template(f: Facts, locale: str = "fr") -> Narrative:
    """Synthèse déterministe (sans IA), toujours disponible."""

    def m(v: Decimal) -> str:
        return money(v, f.currency, locale)

    fr = locale != "en"
    scope = ("à date" if fr else "to date") if f.partial else ""
    trend = percent(f.change_percent, locale)
    if fr:
        headline = f"Dépense {scope + ' ' if scope else ''}de {m(f.total)}, soit {trend} par rapport au mois précédent."
        if not f.previous_complete:
            headline = f"Dépense {scope + ' ' if scope else ''}de {m(f.total)} ; données du mois précédent incomplètes, pas de comparaison."
        summary = (
            f"La dépense cloud de {f.period} {'s élève à date' if f.partial else 's est élevée'} à {m(f.total)} contre {m(f.previous_total)} "
            f"sur la période comparable précédente ({trend})."
        )
        summary = summary.replace("s élève", "s'élève").replace("s est élevée", "s'est élevée")
        if f.forecast_month_end is not None:
            summary += f" La prévision de fin de mois est de {m(f.forecast_month_end)}."
        if f.savings_realized_monthly > 0:
            summary += f" Les optimisations appliquées font économiser {m(f.savings_realized_monthly)} par mois."
        if f.savings_potential_monthly > 0:
            potential = m(f.savings_potential_monthly)
            summary += f" {f.open_recommendations} recommandations ouvertes représentent {potential} d'économies mensuelles potentielles."
    else:
        headline = f"Spend {scope + ' ' if scope else ''}of {m(f.total)}, {trend} versus the previous month."
        if not f.previous_complete:
            headline = f"Spend {scope + ' ' if scope else ''}of {m(f.total)}; previous month data is incomplete, no comparison."
        verb = "is" if f.partial else "was"
        summary = f"Cloud spend for {f.period} {verb} {m(f.total)} versus {m(f.previous_total)} over the previous comparable period ({trend})."
        if not f.previous_complete:
            summary = f"Cloud spend for {f.period} {verb} {m(f.total)}. Previous month data is incomplete: no change is computed."
        if f.forecast_month_end is not None:
            summary += f" The month-end forecast is {m(f.forecast_month_end)}."
        if f.savings_realized_monthly > 0:
            summary += f" Applied optimizations save {m(f.savings_realized_monthly)} per month."
        if f.savings_potential_monthly > 0:
            summary += f" {f.open_recommendations} open recommendations represent {m(f.savings_potential_monthly)} of potential monthly savings."
    variations = []
    for v in f.variations:
        direction = ("hausse" if v["delta"] > 0 else "baisse") if fr else ("up" if v["delta"] > 0 else "down")
        if fr:
            variations.append(f"{v['name']} : {m(v['amount'])} ({direction} de {m(abs(v['delta']))}).")
        else:
            variations.append(f"{v['name']}: {m(v['amount'])} ({direction} {m(abs(v['delta']))}).")
    for a in f.anomalies:
        if a["explanation"]:
            variations.append(a["explanation"])
    actions = []
    for a in f.actions:
        actions.append(f"{a['title']} — {m(a['savings_monthly'])}{' par mois' if fr else ' per month'}.")
    return Narrative(headline=headline, summary=summary, variations=variations[:5], actions=actions[:3])


def _parse_json(text: str) -> dict[str, Any] | None:
    s = text.strip()
    s = re.sub(r"^```(?:json)?\s*|\s*```$", "", s)
    start, end = s.find("{"), s.rfind("}")
    if start < 0 or end <= start:
        return None
    try:
        data = json.loads(s[start : end + 1])
    except ValueError:
        return None
    return data if isinstance(data, dict) else None


def narrate(provider: Provider, f: Facts, locale: str) -> tuple[Narrative | None, Any]:
    """Synthèse rédigée par l'IA, acceptée seulement si tous ses chiffres figurent dans les faits."""
    facts_json = json.dumps(f.as_json(), ensure_ascii=False)
    # Formes affichées (format local) des montants, pour que le modèle puisse les reprendre telles quelles.
    turn = provider.turn(prompts.report(locale, f.org_name), [{"role": "user", "content": facts_json}], lambda _t: None, use_tools=False)
    if turn.stop != "end":
        log.warning("report narrative not usable: %s %s", turn.stop, turn.error)
        return None, turn.usage
    data = _parse_json(turn.text)
    if data is None:
        return None, turn.usage
    try:
        n = Narrative.model_validate(data)
    except ValidationError:
        return None, turn.usage
    text = "\n".join([n.headline, n.summary, *n.variations, *n.actions])
    report = grounding.check(text, [f.as_json()])
    if not report.ok:
        log.warning("report narrative rejected: ungrounded numbers", extra={"count": len(report.ungrounded)})
        return None, turn.usage
    return n, turn.usage


# --------------------------------------------------------------------------- PDF

_FONT_CANDIDATES = [
    "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
    "/usr/share/fonts/dejavu/DejaVuSans.ttf",
    "/usr/share/fonts/TTF/DejaVuSans.ttf",
    "/Library/Fonts/Arial Unicode.ttf",
    "C:/Windows/Fonts/arial.ttf",
    "C:/Windows/Fonts/segoeui.ttf",
]


def _font(cfg: Settings) -> tuple[str, str] | None:
    """Police Unicode (regular, bold) ; None → police intégrée Latin-1."""
    for p in [cfg.report_font, *_FONT_CANDIDATES]:
        if p and os.path.isfile(p):
            bold = ""
            for cand in (
                p.replace("DejaVuSans.ttf", "DejaVuSans-Bold.ttf"),
                p.replace("arial.ttf", "arialbd.ttf"),
                p.replace("segoeui.ttf", "segoeuib.ttf"),
            ):
                if cand != p and os.path.isfile(cand):
                    bold = cand
            return p, bold or p
    return None


_BRAND = (37, 99, 235)
_MUTED = (100, 116, 139)
_TEXT = (15, 23, 42)


@dataclass(frozen=True)
class Brand:
    """Identité du rapport : Kairn, ou marque blanche d'un MSP (M-11)."""

    name: str = "Kairn"
    color: tuple[int, int, int] = _BRAND
    support_email: str = ""
    white_label: bool = False


def _hex_color(value: str) -> tuple[int, int, int] | None:
    m = re.fullmatch(r"#?([0-9a-fA-F]{6})", value.strip())
    if not m:
        return None
    h = m.group(1)
    return int(h[0:2], 16), int(h[2:4], 16), int(h[4:6], 16)


def brand_for(api: ServiceAPI | None, org: dict[str, Any]) -> Brand:
    """Marque blanche de l'organisation, sinon celle de son MSP parent.

    Le logo n'est pas téléchargé dans le PDF (aucun appel sortant vers une URL
    fournie par un client) : nom, couleur et e-mail de support suffisent.
    """
    wl = (org.get("settings") or {}).get("white_label")
    parent = org.get("parent_org_id")
    if not wl and parent and api is not None:
        try:
            wl = (api.org(str(parent)).get("settings") or {}).get("white_label")
        except KairnError:
            wl = None
    if not isinstance(wl, dict):
        return Brand()
    name = re.sub(r"[\x00-\x1f\x7f]", "", str(wl.get("company_name") or "")).strip()
    if not name:
        return Brand()
    return Brand(
        name=name[:60],
        color=_hex_color(str(wl.get("primary_color") or "")) or _BRAND,
        support_email=str(wl.get("support_email") or "").strip()[:120],
        white_label=True,
    )


class _PDF(FPDF):
    def __init__(self, font: tuple[str, str] | None, footer_text: str) -> None:
        super().__init__(format="A4")
        self._footer_text = footer_text
        if font:
            self.add_font("Kairn", "", font[0])
            self.add_font("Kairn", "B", font[1])
            self.ff = "Kairn"
        else:
            self.ff = "Helvetica"
        self.set_auto_page_break(auto=True, margin=18)
        self.set_margins(18, 18, 18)

    def safe(self, text: str) -> str:
        if self.ff == "Kairn":
            return text
        repl = {"\u202f": " ", "\u00a0": " ", "€": "EUR", "—": "-", "–": "-", "’": "'", "«": '"', "»": '"', "…": "..."}
        for k, v in repl.items():
            text = text.replace(k, v)
        return text.encode("latin-1", "replace").decode("latin-1")

    def footer(self) -> None:
        self.set_y(-12)
        self.set_font(self.ff, "", 7.5)
        self.set_text_color(*_MUTED)
        self.cell(0, 5, self.safe(f"{self._footer_text} — {self.page_no()}"), align="C")

    def heading(self, text: str, size: float = 12.5) -> None:
        self.ln(3)
        self.set_font(self.ff, "B", size)
        self.set_text_color(*_TEXT)
        self.cell(0, 7, self.safe(text), new_x=XPos.LMARGIN, new_y=YPos.NEXT)
        self.set_draw_color(226, 232, 240)
        self.line(self.l_margin, self.get_y(), self.w - self.r_margin, self.get_y())
        self.ln(2)

    def para(self, text: str, size: float = 10, color: tuple[int, int, int] = _TEXT) -> None:
        self.set_font(self.ff, "", size)
        self.set_text_color(*color)
        self.multi_cell(0, 5.2, self.safe(text), align="L", new_x=XPos.LMARGIN, new_y=YPos.NEXT)

    def bullets(self, items: list[str], numbered: bool = False) -> None:
        self.set_font(self.ff, "", 10)
        self.set_text_color(*_TEXT)
        for i, it in enumerate(items, 1):
            mark = f"{i}." if numbered else "•" if self.ff == "Kairn" else "-"
            self.cell(7, 5.2, mark)
            self.multi_cell(0, 5.2, self.safe(it), align="L", new_x=XPos.LMARGIN, new_y=YPos.NEXT)
            self.ln(0.8)


def render_pdf(cfg: Settings, f: Facts, n: Narrative, locale: str, generated_by: str, brand: Brand | None = None) -> bytes:
    fr = locale != "en"
    brand = brand or Brand()

    def m(v: Decimal) -> str:
        return money(v, f.currency, locale)

    footer = brand.name + (" — rapport mensuel " if fr else " — monthly report ") + f.period
    if brand.white_label:
        if brand.support_email:
            footer += (" — contact : " if fr else " — contact: ") + brand.support_email
    else:
        footer += " — montants traçables dans Kairn" if fr else " — amounts traceable in Kairn"
    pdf = _PDF(_font(cfg), footer)
    pdf.add_page()
    # En-tête
    pdf.set_fill_color(*brand.color)
    pdf.rect(0, 0, pdf.w, 3, style="F")
    pdf.set_font(pdf.ff, "B", 9)
    pdf.set_text_color(*brand.color)
    pdf.cell(0, 5, pdf.safe(brand.name.upper()), new_x=XPos.LMARGIN, new_y=YPos.NEXT)
    pdf.set_font(pdf.ff, "B", 19)
    pdf.set_text_color(*_TEXT)
    title = (f"Rapport mensuel — {f.period}" if fr else f"Monthly report — {f.period}") + (
        " (à date)" if fr and f.partial else " (to date)" if f.partial else ""
    )
    pdf.cell(0, 10, pdf.safe(title), new_x=XPos.LMARGIN, new_y=YPos.NEXT)
    pdf.para(f.org_name, 10, _MUTED)
    pdf.ln(2)
    pdf.set_font(pdf.ff, "B", 12)
    pdf.set_text_color(*_TEXT)
    pdf.multi_cell(0, 6, pdf.safe(n.headline), align="L", new_x=XPos.LMARGIN, new_y=YPos.NEXT)
    pdf.ln(3)

    # Indicateurs clés
    kpis = [
        ("Dépense" if fr else "Spend", m(f.total)),
        ("Évolution" if fr else "Change", percent(f.change_percent, locale)),
        ("Prévision fin de mois" if fr else "Month-end forecast", m(f.forecast_month_end))
        if f.forecast_month_end is not None
        else ("Mois précédent" if fr else "Previous month", m(f.previous_total)),
        ("Économies réalisées / mois" if fr else "Realized savings / month", m(f.savings_realized_monthly)),
    ]
    w = (pdf.w - pdf.l_margin - pdf.r_margin - 3 * 3) / 4
    y = pdf.get_y()
    for i, (label, value) in enumerate(kpis):
        x = pdf.l_margin + i * (w + 3)
        pdf.set_fill_color(241, 245, 249)
        pdf.rect(x, y, w, 19, style="F")
        pdf.set_xy(x + 3, y + 2.5)
        pdf.set_font(pdf.ff, "", 7.5)
        pdf.set_text_color(*_MUTED)
        pdf.cell(w - 6, 4, pdf.safe(label))
        pdf.set_xy(x + 3, y + 8)
        pdf.set_font(pdf.ff, "B", 12.5)
        pdf.set_text_color(*_TEXT)
        pdf.cell(w - 6, 7, pdf.safe(value))
    pdf.set_xy(pdf.l_margin, y + 23)

    pdf.heading("Synthèse" if fr else "Summary")
    pdf.para(n.summary)

    if f.by_team:
        pdf.heading("Répartition par équipe" if fr else "Breakdown by team")
        top = max((r["amount"] for r in f.by_team), default=Decimal(1)) or Decimal(1)
        bar_w = 70.0
        for r in f.by_team:
            y = pdf.get_y()
            pdf.set_font(pdf.ff, "", 9.5)
            pdf.set_text_color(*_TEXT)
            pdf.cell(52, 6, pdf.safe(str(r["name"])[:34]))
            pdf.set_fill_color(*brand.color)
            pdf.rect(pdf.get_x(), y + 1.5, max(0.5, float(r["amount"] / top) * bar_w), 3, style="F")
            pdf.set_x(pdf.get_x() + bar_w + 3)
            pdf.cell(30, 6, pdf.safe(m(r["amount"])), align="R")
            pdf.set_text_color(*_MUTED)
            pdf.cell(0, 6, pdf.safe(percent(r["change_percent"], locale)), align="R", new_x=XPos.LMARGIN, new_y=YPos.NEXT)

    if n.variations:
        pdf.heading("Principales variations" if fr else "Main changes")
        pdf.bullets(n.variations)

    pdf.heading("Économies" if fr else "Savings")
    pdf.para(
        (
            f"Réalisées : {m(f.savings_realized_monthly)} par mois · acceptées : {m(f.savings_accepted_monthly)} par mois · "
            f"potentielles : {m(f.savings_potential_monthly)} par mois ({f.open_recommendations} recommandations ouvertes)."
        )
        if fr
        else (
            f"Realized: {m(f.savings_realized_monthly)} per month · accepted: {m(f.savings_accepted_monthly)} per month · "
            f"potential: {m(f.savings_potential_monthly)} per month ({f.open_recommendations} open recommendations)."
        )
    )

    if n.actions:
        pdf.heading("Trois actions prioritaires" if fr else "Three priority actions")
        pdf.bullets(n.actions, numbered=True)

    if f.budgets:
        pdf.heading("Budgets")
        for b in f.budgets:
            pdf.para(
                (f"{b['name']} : {m(b['actual'])} consommés sur {m(b['amount'])}, prévision {m(b['forecast'])}")
                if fr
                else (f"{b['name']}: {m(b['actual'])} spent of {m(b['amount'])}, forecast {m(b['forecast'])}")
            )

    pdf.ln(4)
    note = (
        "Tous les montants proviennent des données Kairn (factures importées, grilles tarifaires versionnées, métriques) et sont "
        "consultables en détail dans l'application. "
        if fr
        else "All amounts come from Kairn data (imported invoices, versioned price lists, metrics) and can be explored in detail in the app. "
    )
    note += (
        ("Synthèse rédigée par IA à partir de ces seules données." if fr else "Summary written by AI from this data only.")
        if generated_by != "template"
        else ""
    )
    pdf.para(f"{note} {cfg.public_url}", 7.5, _MUTED)
    return bytes(pdf.output())


# --------------------------------------------------------------------------- orchestration


def generate(cfg: Settings, api: ServiceAPI, org_id: str, period: str, provider: Provider | None = None, today: date | None = None) -> dict[str, Any]:
    """Construit et dépose le rapport ; renvoie le résumé enregistré."""
    org = api.org(org_id)
    locale = "en" if org.get("locale") == "en" else "fr"
    try:
        facts = collect(api, org, period, today)
    except KairnError as exc:
        api.put(f"/internal/v1/orgs/{org_id}/reports/{period}", {"status": "error", "summary": {"error": exc.detail[:300]}})
        raise
    narrative: Narrative | None = None
    generated_by = "template"
    settings = org.get("settings") or {}
    if os.environ.get("KAIRN_REPORTS_AI", "1") != "0":
        try:
            prov = provider or for_org(cfg, str(settings.get("llm_provider") or ""), bool(settings.get("allow_external_llm")))
            narrative, usage = narrate(prov, facts, locale)
            if usage.input_tokens or usage.output_tokens:
                api.record_usage(org_id, usage_payload(usage, "report"))
            if narrative is not None:
                generated_by = prov.name
        except ProviderUnavailable as exc:
            log.info("report without AI narrative: %s", exc)
    if narrative is None:
        narrative = template(facts, locale)
    brand = brand_for(api, org)
    pdf = render_pdf(cfg, facts, narrative, locale, generated_by, brand)
    summary = {
        "brand": brand.name,
        "headline": narrative.headline,
        "summary": narrative.summary,
        "variations": narrative.variations,
        "actions": narrative.actions,
        "total": str(facts.total),
        "previous_total": str(facts.previous_total),
        "currency": facts.currency,
        "change_percent": str(facts.change_percent) if facts.change_percent is not None else None,
        "savings_realized_monthly": str(facts.savings_realized_monthly),
        "generated_by": generated_by,
        "partial": facts.partial,
    }
    api.put(f"/internal/v1/orgs/{org_id}/reports/{period}", {"status": "ready", "summary": summary, "pdf_base64": base64.b64encode(pdf).decode()})
    if os.environ.get("KAIRN_REPORT_DEBUG_DIR"):
        Path(os.environ["KAIRN_REPORT_DEBUG_DIR"], f"{org_id}-{period}.pdf").write_bytes(pdf)
    return summary
