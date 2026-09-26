from __future__ import annotations

from kairn_ai.grounding import check, numbers_in_text


def test_extracts_formats() -> None:
    found = {tok for tok, _ in numbers_in_text("Total 2 479,64 € (+41,1 %), soit 1,234.50 $ et 12.5 cœurs le 13/09/2026 à 10:12.")}
    assert "2 479,64" in found and "41,1" in found and "1,234.50" in found and "12.5" in found
    assert not any("2026" in t or t == "13" for t in found)


def test_grounded_answer() -> None:
    sources = [{"month_to_date": "2479.640000", "change_percent": "41.1", "rows": [{"amount": "1184.28"}]}]
    rep = check("Vous avez dépensé 2 479,64 € ce mois-ci (+41,1 %). L'équipe Data coûte 1 184,28 €.", sources)
    assert rep.ok, rep.ungrounded


def test_rounded_and_percent_ratio() -> None:
    sources = [{"total": "2999.964", "cpu_p95": 0.0414}]
    rep = check("Prévision : environ 3 000 € ; CPU P95 à 4,1 %.", sources)
    assert rep.ok, rep.ungrounded


def test_detects_invented_number() -> None:
    sources = [{"month_to_date": "2479.64"}]
    rep = check("Vous avez dépensé 2 479,64 € et économiserez 812,00 € par mois.", sources)
    assert rep.ungrounded == ["812,00"]


def test_question_numbers_allowed() -> None:
    rep = check("Avec 450 € de budget, vous êtes à 2 479,64 €.", [{"a": "2479.64"}], question="Mon budget est de 450 €")
    assert rep.ok
