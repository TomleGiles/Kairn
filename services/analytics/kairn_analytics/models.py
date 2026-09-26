"""Schémas échangés avec l'API interne (miroir de pkg/model)."""

from __future__ import annotations

from datetime import datetime
from decimal import Decimal
from typing import Any

from pydantic import BaseModel, ConfigDict, Field


class _Model(BaseModel):
    model_config = ConfigDict(extra="ignore")


class OrgSettings(_Model):
    llm_provider: str = ""
    allow_external_llm: bool = False
    rightsizing_percentile: int = 0
    rightsizing_window_days: int = 0


class Limits(_Model):
    plan: str = ""
    features: dict[str, bool] = Field(default_factory=dict)


class Organization(_Model):
    id: str
    name: str
    currency: str = "EUR"
    locale: str = "fr"
    timezone: str = "Europe/Paris"
    plan: str = "starter"
    settings: OrgSettings = Field(default_factory=OrgSettings)
    limits: Limits = Field(default_factory=Limits)

    def allows(self, feature: str) -> bool:
        return bool(self.limits.features.get(feature, False))


class Resource(_Model):
    id: str
    connector_id: str = ""
    provider: str = ""
    type: str
    external_id: str = ""
    name: str = ""
    region: str = ""
    attributes: dict[str, Any] = Field(default_factory=dict)
    labels: dict[str, str] = Field(default_factory=dict)
    valid_from: datetime
    valid_to: datetime | None = None

    def attr(self, key: str) -> str:
        v = self.attributes.get(key)
        if v is None:
            return ""
        if isinstance(v, float) and v.is_integer():
            return str(int(v))
        return str(v)

    def attr_num(self, key: str) -> float:
        try:
            return float(self.attributes.get(key) or 0)
        except (TypeError, ValueError):
            return 0.0


class Edge(_Model):
    parent_id: str
    child_id: str
    relation: str
    valid_from: datetime
    valid_to: datetime | None = None


class Point(_Model):
    ts: datetime
    value: float


class Series(_Model):
    resource_id: str
    metric: str
    points: list[Point] = Field(default_factory=list)


class CostRow(_Model):
    period: datetime
    keys: dict[str, str] = Field(default_factory=dict)
    amount: Decimal
    currency: str = "EUR"


class Event(_Model):
    ts: datetime
    kind: str
    source: str = ""
    resource_id: str = ""
    title: str = ""
    payload: dict[str, Any] = Field(default_factory=dict)


class CatalogItem(_Model):
    provider: str
    version: str = ""
    sku: str
    region: str = ""
    unit: str
    price: Decimal
    currency: str = "EUR"
    attributes: dict[str, str] = Field(default_factory=dict)


class Remediation(_Model):
    steps: list[str] = Field(default_factory=list)
    cli: str = ""
    manifest: str = ""
    terraform: str = ""


class Recommendation(_Model):
    id: str = ""
    type: str
    resource_id: str
    fingerprint: str
    title: str
    summary: str = ""
    savings_monthly: Decimal
    currency: str = "EUR"
    risk: str = "low"
    status: str = "open"
    evidence: dict[str, Any] = Field(default_factory=dict)
    remediation: Remediation = Field(default_factory=Remediation)
    applied_at: datetime | None = None
    measured_savings_monthly: Decimal | None = None


class CorrelatedEvent(_Model):
    event: Event
    score: float
    why: str


class Anomaly(_Model):
    id: str = ""
    series_key: str
    kind: str = "cost"
    title: str
    window_start: datetime
    window_end: datetime
    severity: str
    expected: Decimal
    actual: Decimal
    currency: str = "EUR"
    score: float
    correlated_events: list[CorrelatedEvent] = Field(default_factory=list)
    explanation: str = ""
    explanation_sources: list[str] = Field(default_factory=list)
    status: str = "open"


class ForecastPoint(_Model):
    day: datetime
    value: Decimal
    lower: Decimal
    upper: Decimal


class Forecast(_Model):
    node_id: str = ""
    generated_at: datetime
    model: str
    currency: str = "EUR"
    points: list[ForecastPoint] = Field(default_factory=list)
    period_end: datetime
    total: Decimal
    lower: Decimal
    upper: Decimal


class AllocationNode(_Model):
    id: str
    parent_id: str | None = None
    kind: str
    name: str
